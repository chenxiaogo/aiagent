package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT 鉴权组件。
type JWT struct {
	secret      string
	expireHours int
}

// Claims 自定义 JWT Claims。
type Claims struct {
	UserID   int64    `json:"uid"`
	Username string   `json:"username"`
	IsAdmin  bool     `json:"isAdmin"`
	RoleID   int64    `json:"roleId"`
	RoleName string   `json:"roleName"`
	Perms    []string `json:"perms"`
	jwt.RegisteredClaims
}

// New 创建 JWT 实例。
func New(secret string, expireHours int) *JWT {
	if expireHours <= 0 {
		expireHours = 24
	}
	return &JWT{secret: secret, expireHours: expireHours}
}

// Generate 签发 token。
func (j *JWT) Generate(userID int64, username string, isAdmin bool, roleID int64, roleName string, perms []string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		IsAdmin:  isAdmin,
		RoleID:   roleID,
		RoleName: roleName,
		Perms:    perms,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(j.expireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "aiagent",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.secret))
}

// Parse 解析 token 返回 Claims。
func (j *JWT) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(j.secret), nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token")
}