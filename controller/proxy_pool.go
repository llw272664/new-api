package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ListProxyPoolEntries returns all proxy pool entries (admin only).
func ListProxyPoolEntries(c *gin.Context) {
	group := c.Query("group")
	var entries []model.ProxyPoolEntry
	q := model.DB
	if group != "" {
		q = q.Where("group_name = ?", group)
	}
	q.Find(&entries)

	// Mask passwords for display
	type entryView struct {
		model.ProxyPoolEntry
		Password    string `json:"password"`
		MaskedURL   string `json:"masked_url"`
		PasswordEnc string `json:"password_enc,omitempty"`
	}
	views := make([]entryView, len(entries))
	for i, e := range entries {
		views[i] = entryView{
			ProxyPoolEntry: entries[i],
			Password:       "",
			MaskedURL:      e.MaskedURL(),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    views,
		"total":   len(views),
	})
}

// CreateProxyPoolEntry creates a new proxy pool entry.
func CreateProxyPoolEntryHandler(c *gin.Context) {
	var entry model.ProxyPoolEntry
	if err := c.ShouldBindJSON(&entry); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	// Validate required fields
	if entry.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "host is required"})
		return
	}
	if entry.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "name is required"})
		return
	}
	if entry.Port <= 0 || entry.Port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "port must be between 1 and 65535"})
		return
	}
	switch entry.Type {
	case "http", "https", "socks5", "socks5h":
	default:
		if entry.Type == "" {
			entry.Type = "http"
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "type must be http, https, socks5, or socks5h"})
			return
		}
	}

	if err := model.CreateProxyPoolEntry(&entry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("failed to create proxy pool entry: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entry,
		"message": "proxy pool entry created",
	})
}

// UpdateProxyPoolEntryHandler updates an existing proxy pool entry.
func UpdateProxyPoolEntryHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid id"})
		return
	}

	var entry model.ProxyPoolEntry
	if err := c.ShouldBindJSON(&entry); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}
	entry.ID = uint(id)

	if err := model.UpdateProxyPoolEntry(&entry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("failed to update proxy pool entry: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entry,
		"message": "proxy pool entry updated",
	})
}

// DeleteProxyPoolEntryHandler deletes a proxy pool entry.
func DeleteProxyPoolEntryHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid id"})
		return
	}

	if err := model.DeleteProxyPoolEntry(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("failed to delete proxy pool entry: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "proxy pool entry deleted",
	})
}

// GetProxyPoolEntryHandler returns a single proxy pool entry.
func GetProxyPoolEntryHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid id"})
		return
	}

	entry, err := model.GetProxyPoolEntryByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "entry not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"id":         entry.ID,
			"name":       entry.Name,
			"type":       entry.Type,
			"host":       entry.Host,
			"port":       entry.Port,
			"username":   entry.Username,
			"weight":     entry.Weight,
			"enabled":    entry.Enabled,
			"group_name": entry.GroupName,
			"status":     entry.Status,
			"fail_count": entry.FailCount,
			"last_check": entry.LastCheck,
			"last_used":  entry.LastUsed,
			"masked_url": entry.MaskedURL(),
			"created_at": entry.CreatedAt,
			"updated_at": entry.UpdatedAt,
		},
	})
}

// CheckProxyPoolEntryHealth runs a health check on a single proxy.
func CheckProxyPoolEntryHealth(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid id"})
		return
	}

	entry, err := model.GetProxyPoolEntryByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "entry not found"})
		return
	}

	healthy := service.CheckProxyHealth(entry)
	status := "unhealthy"
	if healthy {
		status = "healthy"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"status":  status,
		"message": fmt.Sprintf("proxy %s is %s", entry.Name, status),
	})
}

// BatchCheckProxyPoolHealth runs health checks on all proxies in a group.
func BatchCheckProxyPoolHealth(c *gin.Context) {
	group := c.Query("group")
	if group == "" {
		group = "default"
	}

	entries := model.GetProxyPoolEntriesByGroup(group)
	if len(entries) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    []interface{}{},
			"message": "no entries in group",
		})
		return
	}

	type result struct {
		ID      uint   `json:"id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		Healthy bool   `json:"healthy"`
	}
	results := make([]result, len(entries))
	for i, entry := range entries {
		healthy := service.CheckProxyHealth(entry)
		status := "unhealthy"
		if healthy {
			status = "healthy"
		}
		results[i] = result{
			ID:      entry.ID,
			Name:    entry.Name,
			Status:  status,
			Healthy: healthy,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    results,
	})
}

// GetProxyPoolGroups returns all proxy pool groups.
func GetProxyPoolGroups(c *gin.Context) {
	groups := model.GetAllProxyPoolGroups()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    groups,
	})
}

// GetProxyPoolSettingsHandler returns proxy pool settings.
func GetProxyPoolSettingsHandler(c *gin.Context) {
	settings := service.GetProxyPoolSettings()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    settings,
	})
}

// UpdateProxyPoolSettingsHandler updates proxy pool settings.
func UpdateProxyPoolSettingsHandler(c *gin.Context) {
	var settings service.ProxyPoolSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	if err := service.SetProxyPoolSettings(settings); err != nil {
		common.ApiError(c, err)
		return
	}
	if settings.Enabled {
		service.RunHealthCheckLoop()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "proxy pool settings updated",
	})
}

// GetProxyPoolStatus returns overall proxy pool status.
func GetProxyPoolStatus(c *gin.Context) {
	settings := service.GetProxyPoolSettings()
	count := model.GetProxyPoolEntryCount()
	groups := model.GetAllProxyPoolGroups()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":  settings.Enabled,
			"total":    count,
			"groups":   groups,
			"strategy": settings.Strategy,
		},
	})
}

// InitProxyPool is called during startup to initialize the proxy pool.
func InitProxyPool() {
	service.LoadProxyPoolSettingsFromDB()
	model.MigrateProxyPool()
	model.LoadProxyPoolCache()
	settings := service.GetProxyPoolSettings()
	if settings.Enabled {
		service.RunHealthCheckLoop()
	}
}
