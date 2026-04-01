package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lyp256/airouter/internal/api/middleware"
	"github.com/lyp256/airouter/internal/crypto"
	"github.com/lyp256/airouter/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserHandler 用户处理器
type UserHandler struct {
	db        *gorm.DB
	encryptor *crypto.Encryptor
}

// NewUserHandler 创建用户处理器
func NewUserHandler(db *gorm.DB, encryptor *crypto.Encryptor) *UserHandler {
	return &UserHandler{db: db, encryptor: encryptor}
}

// ListUsers 列出用户
func (h *UserHandler) ListUsers(c *gin.Context) {
	page := 1
	pageSize := 20

	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	var total int64
	h.db.Model(&model.User{}).Count(&total)

	var users []model.User
	offset := (page - 1) * pageSize
	if err := h.db.Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      users,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetUser 获取用户详情
func (h *UserHandler) GetUser(c *gin.Context) {
	id := c.Param("id")

	var user model.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	// 获取用户密钥列表
	var keys []model.UserKey
	h.db.Where("user_id = ?", id).Find(&keys)

	c.JSON(http.StatusOK, gin.H{
		"data": user,
		"keys": keys,
	})
}

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Role     string `json:"role"`
}

// CreateUser 创建用户
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 检查用户名是否已存在
	var count int64
	h.db.Model(&model.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名已存在"})
		return
	}

	// 检查邮箱是否已存在
	h.db.Model(&model.User{}).Where("email = ?", req.Email).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "邮箱已存在"})
		return
	}

	// 生成密码哈希
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	role := req.Role
	if role == "" {
		role = "user"
	}

	user := model.User{
		ID:        uuid.New().String(),
		Username:  req.Username,
		Email:     req.Email,
		Password:  string(hashedPassword),
		Role:      role,
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": user})
}

// UpdateUserRequest 更新用户请求
type UpdateUserRequest struct {
	Email  string `json:"email"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// UpdateUser 更新用户
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id := c.Param("id")

	var user model.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}

	if req.Email != "" {
		updates["email"] = req.Email
	}
	if req.Role != "" {
		updates["role"] = req.Role
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}

	if err := h.db.Model(&user).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	h.db.First(&user, "id = ?", id)
	c.JSON(http.StatusOK, gin.H{"data": user})
}

// DeleteUser 删除用户
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id := c.Param("id")

	// 开启事务
	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	result := tx.Delete(&model.User{}, "id = ?", id)
	if result.Error != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	// 删除用户密钥
	tx.Delete(&model.UserKey{}, "user_id = ?", id)

	// 删除用户的使用日志（将 user_id 设为空，保留日志用于统计）
	tx.Model(&model.UsageLog{}).Where("user_id = ?", id).Update("user_id", "")

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// === 用户密钥管理 ===

// ListUserKeys 列出用户密钥
// 管理员：必须指定 user_id 参数
// 普通用户：只能查看自己的密钥
func (h *UserHandler) ListUserKeys(c *gin.Context) {
	currentUserID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	userID := c.Query("user_id")

	// 权限检查：普通用户只能查看自己的密钥
	if !isAdmin {
		if userID == "" {
			// 普通用户未指定 user_id，返回自己的密钥
			userID = currentUserID
		} else if userID != currentUserID {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足，只能查看自己的密钥"})
			return
		}
	}

	// 管理员必须指定 user_id
	if isAdmin && userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 user_id 参数"})
		return
	}

	var keys []model.UserKey
	if err := h.db.Where("user_id = ?", userID).Find(&keys).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": keys})
}

// GetMyKeys 获取当前用户的密钥列表
// 用于前端聊天功能选择用户自己的密钥
func (h *UserHandler) GetMyKeys(c *gin.Context) {
	// 从上下文获取当前用户 ID（JWT 认证后注入）
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var keys []model.UserKey
	if err := h.db.Where("user_id = ?", userID).Find(&keys).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": keys})
}

// CreateUserKeyRequest 创建用户密钥请求
type CreateUserKeyRequest struct {
	Name        string     `json:"name" binding:"required"`
	UserID      string     `json:"user_id" binding:"required"`
	Permissions string     `json:"permissions"`
	RateLimit   int        `json:"rate_limit"`
	QuotaLimit  int64      `json:"quota_limit"`
	ExpiredAt   *time.Time `json:"expired_at"`
}

// CreateUserKey 创建用户密钥
// 管理员：可为任意用户创建密钥
// 普通用户：只能为自己创建密钥
func (h *UserHandler) CreateUserKey(c *gin.Context) {
	var req CreateUserKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	currentUserID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	// 权限检查：普通用户只能为自己创建密钥
	if !isAdmin && req.UserID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足，只能为自己创建密钥"})
		return
	}

	// 检查用户是否存在
	var user model.User
	if err := h.db.First(&user, "id = ?", req.UserID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户不存在"})
		return
	}

	// 生成 API Key
	rawKey := generateAPIKey()
	encryptedKey, err := h.encryptor.Encrypt(rawKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密钥加密失败"})
		return
	}

	key := model.UserKey{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Key:         encryptedKey,
		UserID:      req.UserID,
		Permissions: req.Permissions,
		RateLimit:   req.RateLimit,
		QuotaLimit:  req.QuotaLimit,
		ExpiredAt:   req.ExpiredAt,
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if key.RateLimit == 0 {
		key.RateLimit = 60
	}

	if err := h.db.Create(&key).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"data":    key,
		"raw_key": rawKey, // 仅创建时返回明文密钥
		"message": "请妥善保存密钥，系统不会再次显示",
	})
}

// UpdateUserKeyRequest 更新用户密钥请求
type UpdateUserKeyRequest struct {
	Name        string     `json:"name"`
	Permissions string     `json:"permissions"`
	RateLimit   *int       `json:"rate_limit"`
	QuotaLimit  *int64     `json:"quota_limit"`
	ExpiredAt   *time.Time `json:"expired_at"`
	Status      string     `json:"status"`
}

// UpdateUserKey 更新用户密钥
// 管理员：可更新任意用户密钥
// 普通用户：只能更新自己的密钥
func (h *UserHandler) UpdateUserKey(c *gin.Context) {
	keyID := c.Param("key_id")

	var key model.UserKey
	if err := h.db.First(&key, "id = ?", keyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "密钥不存在"})
		return
	}

	// 权限检查：普通用户只能操作自己的密钥
	currentUserID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && key.UserID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足，只能操作自己的密钥"})
		return
	}

	var req UpdateUserKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Permissions != "" {
		updates["permissions"] = req.Permissions
	}
	if req.RateLimit != nil {
		updates["rate_limit"] = *req.RateLimit
	}
	if req.QuotaLimit != nil {
		updates["quota_limit"] = *req.QuotaLimit
	}
	if req.ExpiredAt != nil {
		updates["expired_at"] = req.ExpiredAt
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}

	if err := h.db.Model(&key).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	h.db.First(&key, "id = ?", keyID)
	c.JSON(http.StatusOK, gin.H{"data": key})
}

// DeleteUserKey 删除用户密钥
// 管理员：可删除任意用户密钥
// 普通用户：只能删除自己的密钥
func (h *UserHandler) DeleteUserKey(c *gin.Context) {
	keyID := c.Param("key_id")

	// 先查询密钥以获取所属用户 ID
	var key model.UserKey
	if err := h.db.First(&key, "id = ?", keyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "密钥不存在"})
		return
	}

	// 权限检查：普通用户只能删除自己的密钥
	currentUserID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && key.UserID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足，只能删除自己的密钥"})
		return
	}

	result := h.db.Delete(&model.UserKey{}, "id = ?", keyID)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "密钥不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// RegenerateUserKey 重新生成用户密钥
// 管理员：可重新生成任意用户密钥
// 普通用户：只能重新生成自己的密钥
func (h *UserHandler) RegenerateUserKey(c *gin.Context) {
	keyID := c.Param("key_id")

	var key model.UserKey
	if err := h.db.First(&key, "id = ?", keyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "密钥不存在"})
		return
	}

	// 权限检查：普通用户只能重新生成自己的密钥
	currentUserID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)
	if !isAdmin && key.UserID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足，只能操作自己的密钥"})
		return
	}

	// 生成新的 API Key
	rawKey := generateAPIKey()
	encryptedKey, err := h.encryptor.Encrypt(rawKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密钥加密失败"})
		return
	}

	// 更新密钥
	if err := h.db.Model(&key).Updates(map[string]interface{}{
		"key":        encryptedKey,
		"updated_at": time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":    key,
		"raw_key": rawKey,
		"message": "请妥善保存新密钥，系统不会再次显示",
	})
}

// generateAPIKey 生成 API Key
func generateAPIKey() string {
	return "sk-" + uuid.New().String()[:32]
}
