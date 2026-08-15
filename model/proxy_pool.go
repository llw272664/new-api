package model

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ProxyPoolEntry represents a single proxy in the system-wide proxy pool.
type ProxyPoolEntry struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name      string `json:"name" gorm:"size:128;not null;uniqueIndex"`
	Type      string `json:"type" gorm:"size:16;not null;default:'http'"`
	Host      string `json:"host" gorm:"size:256;not null"`
	Port      int    `json:"port" gorm:"not null;default:8080"`
	Username  string `json:"username" gorm:"size:256"`
	Password  string `json:"-" gorm:"size:512"`
	Weight    int    `json:"weight" gorm:"not null;default:1"`
	Enabled   bool   `json:"enabled" gorm:"not null;default:true"`
	GroupName string `json:"group_name" gorm:"size:64;not null;default:'default'"`

	Status       string    `json:"status" gorm:"size:16;not null;default:'unknown'"`
	FailCount    int       `json:"fail_count" gorm:"not null;default:0"`
	SuccessCount int       `json:"success_count" gorm:"not null;default:0"`
	LastCheck    time.Time `json:"last_check"`
	LastUsed     time.Time `json:"last_used"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (ProxyPoolEntry) TableName() string {
	return "proxy_pool_entries"
}

func (e *ProxyPoolEntry) MaskedURL() string {
	scheme := e.Type
	if scheme == "" {
		scheme = "http"
	}
	host := e.Host
	if host == "" {
		return ""
	}
	if e.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, e.Port)
	}
	if e.Username != "" {
		return fmt.Sprintf("%s://%s:***@%s", scheme, e.Username, host)
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func (e *ProxyPoolEntry) ToProxyURL() (string, error) {
	scheme := e.Type
	if scheme == "" {
		scheme = "http"
	}
	host := e.Host
	if host == "" {
		return "", fmt.Errorf("proxy host is empty")
	}
	if e.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, e.Port)
	}
	if e.Username != "" {
		decoded, err := DecryptProxyPassword(e.Password)
		if err != nil {
			return "", fmt.Errorf("decrypt password: %w", err)
		}
		if decoded != "" {
			escaped := url.UserPassword(e.Username, decoded).String()
			return fmt.Sprintf("%s://%s@%s", scheme, escaped, host), nil
		}
		return fmt.Sprintf("%s://%s@%s", scheme, e.Username, host), nil
	}
	return fmt.Sprintf("%s://%s", scheme, host), nil
}

var proxyEncryptKey = sha256.Sum256([]byte("new-api-proxy-pool-encrypt-key-2026"))

func aesCipher() (cipher.AEAD, error) {
	block, err := aes.NewCipher(proxyEncryptKey[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func EncryptProxyPassword(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	aead, err := aesCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func DecryptProxyPassword(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}
	aead, err := aesCipher()
	if err != nil {
		return "", err
	}
	nonceSize := aead.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

var proxyPoolCache = struct {
	sync.RWMutex
	entries map[uint]*ProxyPoolEntry
	byGroup map[string][]*ProxyPoolEntry
	rrIndex map[string]uint
}{
	entries: make(map[uint]*ProxyPoolEntry),
	byGroup: make(map[string][]*ProxyPoolEntry),
	rrIndex: make(map[string]uint),
}

func InvalidateProxyPoolCache() {
	proxyPoolCache.Lock()
	defer proxyPoolCache.Unlock()
	proxyPoolCache.entries = make(map[uint]*ProxyPoolEntry)
	proxyPoolCache.byGroup = make(map[string][]*ProxyPoolEntry)
	proxyPoolCache.rrIndex = make(map[string]uint)
}

func LoadProxyPoolCache() []*ProxyPoolEntry {
	proxyPoolCache.Lock()
	defer proxyPoolCache.Unlock()

	proxyPoolCache.entries = make(map[uint]*ProxyPoolEntry)
	proxyPoolCache.byGroup = make(map[string][]*ProxyPoolEntry)

	var entries []*ProxyPoolEntry
	if err := DB.Where("enabled = ?", true).Find(&entries).Error; err != nil {
		common.SysError("failed to load proxy pool entries: " + err.Error())
		return nil
	}

	for i := range entries {
		e := entries[i]
		proxyPoolCache.entries[e.ID] = e
		g := e.GroupName
		if g == "" {
			g = "default"
		}
		proxyPoolCache.byGroup[g] = append(proxyPoolCache.byGroup[g], e)
	}
	return entries
}

// NextProxyPoolRRIndex atomically returns the current round-robin index for a
// group and advances it by one. It is used by the scheduling layer.
func NextProxyPoolRRIndex(group string) uint {
	if group == "" {
		group = "default"
	}
	proxyPoolCache.Lock()
	defer proxyPoolCache.Unlock()
	idx := proxyPoolCache.rrIndex[group]
	proxyPoolCache.rrIndex[group] = idx + 1
	return idx
}

func GetProxyPoolEntriesByGroup(group string) []*ProxyPoolEntry {
	proxyPoolCache.RLock()
	entries := proxyPoolCache.byGroup[group]
	proxyPoolCache.RUnlock()
	if entries == nil {
		return nil
	}
	result := make([]*ProxyPoolEntry, len(entries))
	copy(result, entries)
	return result
}

func GetAllProxyPoolGroups() []string {
	proxyPoolCache.RLock()
	defer proxyPoolCache.RUnlock()
	groups := make(map[string]struct{})
	for _, e := range proxyPoolCache.entries {
		g := e.GroupName
		if g == "" {
			g = "default"
		}
		groups[g] = struct{}{}
	}
	result := make([]string, 0, len(groups))
	for g := range groups {
		result = append(result, g)
	}
	return result
}

func GetProxyPoolEntryByID(id uint) (*ProxyPoolEntry, error) {
	var entry ProxyPoolEntry
	if err := DB.First(&entry, id).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func CreateProxyPoolEntry(entry *ProxyPoolEntry) error {
	if entry.Password != "" {
		encrypted, err := EncryptProxyPassword(entry.Password)
		if err != nil {
			return fmt.Errorf("encrypt password: %w", err)
		}
		entry.Password = encrypted
	}
	if entry.Weight <= 0 {
		entry.Weight = 1
	}
	if entry.GroupName == "" {
		entry.GroupName = "default"
	}
	if entry.Type == "" {
		entry.Type = "http"
	}
	if err := DB.Create(entry).Error; err != nil {
		return err
	}
	LoadProxyPoolCache()
	return nil
}

func UpdateProxyPoolEntry(entry *ProxyPoolEntry) error {
	old, err := GetProxyPoolEntryByID(entry.ID)
	if err != nil {
		return err
	}
	if entry.Password == "" {
		entry.Password = old.Password
	} else {
		encrypted, err := EncryptProxyPassword(entry.Password)
		if err != nil {
			return fmt.Errorf("encrypt password: %w", err)
		}
		entry.Password = encrypted
	}
	if err := DB.Save(entry).Error; err != nil {
		return err
	}
	LoadProxyPoolCache()
	return nil
}

func DeleteProxyPoolEntry(id uint) error {
	if err := DB.Delete(&ProxyPoolEntry{}, id).Error; err != nil {
		return err
	}
	LoadProxyPoolCache()
	return nil
}

func UpdateProxyPoolEntryStatus(id uint, status string, failCount, successCount int) {
	DB.Model(&ProxyPoolEntry{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        status,
		"fail_count":    failCount,
		"success_count": successCount,
		"last_check":    time.Now(),
	})
	proxyPoolCache.Lock()
	if e, ok := proxyPoolCache.entries[id]; ok {
		e.Status = status
		e.FailCount = failCount
		e.SuccessCount = successCount
		e.LastCheck = time.Now()
	}
	proxyPoolCache.Unlock()
}

func RecordProxyPoolEntryUsed(id uint) {
	now := time.Now()
	DB.Model(&ProxyPoolEntry{}).Where("id = ?", id).Update("last_used", now)
	proxyPoolCache.Lock()
	if e, ok := proxyPoolCache.entries[id]; ok {
		e.LastUsed = now
	}
	proxyPoolCache.Unlock()
}

func MigrateProxyPool() {
	if err := DB.AutoMigrate(&ProxyPoolEntry{}); err != nil {
		common.SysError("failed to migrate proxy_pool_entries: " + err.Error())
	}
}

func GetProxyPoolEntryCount() int64 {
	var count int64
	DB.Model(&ProxyPoolEntry{}).Count(&count)
	return count
}
