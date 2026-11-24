package cstoken

import (
	"errors"
	"strings"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
)

// CsClaims 客服业务独立令牌 与平台 GVA 令牌通过 Audience 区分 严禁跨端互认
type CsClaims struct {
	Scope       string `json:"scope"` // platform|merchant|agent|member
	AccountID   uint   `json:"accountId"`
	TenantID    uint   `json:"tenantId"` // platform 端为 0
	Username    string `json:"username"`
	EmployeeNo  string `json:"employeeNo"`
	AuthorityId uint   `json:"authorityId"` // 仅 platform 端使用
	jwt.RegisteredClaims
}

var (
	ErrTokenExpired  = errors.New("token已过期")
	ErrTokenInvalid  = errors.New("无效token")
	ErrScopeMismatch = errors.New("令牌与当前端不匹配")
)

func signingKey() []byte { return []byte(global.GVA_CONFIG.JWT.SigningKey) }

func expires() time.Duration {
	d, err := time.ParseDuration(global.GVA_CONFIG.JWT.ExpiresTime)
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

// CreateToken 签发端内令牌
func CreateToken(scope string, accountID, tenantID uint, username, employeeNo string, authorityID uint) (string, int64, error) {
	claims := CsClaims{
		Scope:       scope,
		AccountID:   accountID,
		TenantID:    tenantID,
		Username:    username,
		EmployeeNo:  employeeNo,
		AuthorityId: authorityID,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{"GVA-CS-" + scope},
			NotBefore: jwt.NewNumericDate(time.Now().Add(-10 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expires())),
			Issuer:    global.GVA_CONFIG.JWT.Issuer,
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signingKey())
	if err != nil {
		return "", 0, err
	}
	return token, claims.ExpiresAt.Unix(), nil
}

func ParseToken(tokenString string) (*CsClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &CsClaims{}, func(t *jwt.Token) (interface{}, error) {
		return signingKey(), nil
	})
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, ErrTokenExpired
		default:
			return nil, ErrTokenInvalid
		}
	}
	if claims, ok := token.Claims.(*CsClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, ErrTokenInvalid
}

// AudienceMatches 校验令牌 Audience 与目标端一致
func AudienceMatches(claims *CsClaims, scope string) bool {
	want := "GVA-CS-" + scope
	for _, a := range claims.Audience {
		if strings.TrimSpace(a) == want {
			return true
		}
	}
	return false
}

// GetToken 优先 x-token 头 其次 cookie 与框架行为一致
func GetToken(c *gin.Context) string {
	token := c.Request.Header.Get("x-token")
	if token == "" {
		token, _ = c.Cookie("x-token")
	}
	return token
}
