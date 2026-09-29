// Package mall 是完整商城版本。原 internal/store 等包保留用于基础课程对照。
package mall

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Fault struct {
	Code    string
	Message string
	Status  int
}

func (e *Fault) Error() string            { return e.Message }
func bad(message string) error            { return &Fault{"invalid_argument", message, 400} }
func conflict(code, message string) error { return &Fault{code, message, 409} }

var notFound = &Fault{"not_found", "资源不存在或不属于当前用户", 404}
var unavailable = &Fault{"unavailable", "服务暂时不可用，请保留原请求编号重试", 503}
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func validKey(k string) bool { return keyPattern.MatchString(k) }
func nowMS() int64           { return time.Now().UTC().UnixMilli() }
func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type User struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"created_at"`
}
type Product struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Subtitle      string `json:"subtitle"`
	Description   string `json:"description"`
	Category      string `json:"category"`
	Image         string `json:"image"`
	Price         int64  `json:"price_cents"`
	OriginalPrice int64  `json:"original_price_cents"`
	Stock         int64  `json:"stock"`
	InitialStock  int64  `json:"initial_stock"`
	Status        string `json:"status"`
	Featured      bool   `json:"featured"`
	CreatedAt     int64  `json:"created_at"`
}
type Address struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}

func (a Address) Validate() error {
	if len([]rune(strings.TrimSpace(a.Recipient))) < 1 || len([]rune(a.Recipient)) > 40 || len(a.Phone) < 6 || len(a.Phone) > 24 || len([]rune(a.Region)) < 2 || len([]rune(a.Region)) > 80 || len([]rune(a.Detail)) < 3 || len([]rune(a.Detail)) > 180 {
		return bad("请填写完整收件人、电话、地区和详细地址")
	}
	return nil
}

type CartItem struct {
	Product  Product `json:"product"`
	Quantity int64   `json:"quantity"`
}
type Line struct {
	ProductID int64 `json:"product_id"`
	Quantity  int64 `json:"quantity"`
}
type OrderItem struct {
	ProductID int64  `json:"product_id"`
	Name      string `json:"name"`
	Image     string `json:"image"`
	Price     int64  `json:"price_cents"`
	Quantity  int64  `json:"quantity"`
}
type Order struct {
	ID         string      `json:"id"`
	UserID     int64       `json:"user_id"`
	Kind       string      `json:"kind"`
	Status     string      `json:"status"`
	Total      int64       `json:"total_cents"`
	Items      []OrderItem `json:"items"`
	Address    Address     `json:"address"`
	ActivityID int64       `json:"activity_id,omitempty"`
	TicketID   string      `json:"ticket_id,omitempty"`
	CreatedAt  int64       `json:"created_at"`
	ExpiresAt  int64       `json:"expires_at"`
	PaidAt     int64       `json:"paid_at"`
	Tracking   string      `json:"tracking"`
	Replayed   bool        `json:"replayed,omitempty"`
}
type Activity struct {
	ID            int64  `json:"id"`
	ProductID     int64  `json:"product_id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Price         int64  `json:"price_cents"`
	OriginalPrice int64  `json:"original_price_cents"`
	InitialStock  int64  `json:"initial_stock"`
	Stock         int64  `json:"stock"`
	Returned      int64  `json:"returned"`
	StartsAt      int64  `json:"starts_at"`
	EndsAt        int64  `json:"ends_at"`
	Status        string `json:"status"`
	Epoch         string `json:"-"`
	Available     int64  `json:"available"`
	Ready         bool   `json:"ready"`
}
type Ticket struct {
	ID         string  `json:"id"`
	UserID     int64   `json:"user_id"`
	ActivityID int64   `json:"activity_id"`
	Epoch      string  `json:"-"`
	Address    Address `json:"-"`
	RequestKey string  `json:"-"`
	Status     string  `json:"status"`
	Reason     string  `json:"reason"`
	OrderID    string  `json:"order_id,omitempty"`
	CreatedAt  int64   `json:"created_at"`
}
type Event struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Payload  string `json:"payload"`
	Attempts int    `json:"attempts"`
}
type Release struct {
	ActivityID int64  `json:"activity_id"`
	TicketID   string `json:"ticket_id"`
	Epoch      string `json:"epoch"`
}
type Session struct {
	User      User   `json:"user"`
	CSRF      string `json:"csrf_token"`
	ExpiresAt int64  `json:"expires_at"`
}
type Page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}

func ticketID(user, activity int64, key string) string {
	return digest(fmt.Sprintf("%d/%d/%s", user, activity, key))[:40]
}
