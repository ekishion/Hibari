package baidu

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"time"
)

// fetchUserSession 从百度接口获取 uk 与 sk
func (c *Client) fetchUserSession() (int64, string, error) {
	// 1. 尝试从 /youth/api/user/getinfo 获取 uk/sk
	apiURL := fmt.Sprintf(
		c.baseURL+"/youth/api/user/getinfo?app_id=%s&clienttype=0&web=1&need_selfinfo=1",
		url.QueryEscape(c.cfg.BaiduAppID),
	)
	body, err := c.BaiduGet(apiURL, "")
	var uk int64
	var sk string
	if err == nil {
		var resp struct {
			Errno   int `json:"errno"`
			Records []struct {
				Uk int64  `json:"uk"`
				Sk string `json:"sk"`
			} `json:"records"`
		}
		if json.Unmarshal(body, &resp) == nil && len(resp.Records) > 0 {
			uk = resp.Records[0].Uk
			sk = resp.Records[0].Sk
		}
	}

	// 2. 如果不完整，从 /api/gettemplatevariable 获取
	if uk == 0 || sk == "" {
		fallbackURL := c.baseURL + `/api/gettemplatevariable?fields=["bdstoken","uk","sk"]`
		body2, err2 := c.BaiduGet(fallbackURL, "")
		if err2 == nil {
			var resp2 struct {
				Result struct {
					Uk int64  `json:"uk"`
					Sk string `json:"sk"`
				} `json:"result"`
			}
			if json.Unmarshal(body2, &resp2) == nil {
				if uk == 0 {
					uk = resp2.Result.Uk
				}
				if sk == "" {
					sk = resp2.Result.Sk
				}
			}
		}
	}

	// 3. 如果还是没有 sk，从 /youth/api/report/user 获取
	if sk == "" && uk != 0 {
		skURL := fmt.Sprintf(
			c.baseURL+"/youth/api/report/user?app_id=%s&clienttype=0&web=1&action=sapi_auth&timestamp=%d",
			url.QueryEscape(c.cfg.BaiduAppID),
			time.Now().UnixMilli(),
		)
		bodySK, errSK := c.BaiduGet(skURL, "")
		if errSK == nil {
			var respSK struct {
				Uinfo string `json:"uinfo"`
			}
			if json.Unmarshal(bodySK, &respSK) == nil && respSK.Uinfo != "" {
				sk = respSK.Uinfo
			}
		}
	}

	if uk == 0 || sk == "" {
		return 0, "", fmt.Errorf("无法从百度网盘获取完整的 Session uk=%d，sk 缺失或无效", uk)
	}

	return uk, sk, nil
}

// GetSession 线程安全地获取或刷新 uk/sk（含 TTL）
func (c *Client) GetSession() (int64, string, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	ttl := c.cfg.SessionTTL()
	if c.uk != 0 && c.sk != "" && !c.sessionAt.IsZero() && time.Since(c.sessionAt) < ttl {
		return c.uk, c.sk, nil
	}
	uk, sk, err := c.fetchUserSession()
	if err != nil {
		return 0, "", err
	}
	c.uk = uk
	c.sk = sk
	c.sessionAt = time.Now()
	log.Printf("[Session] 获取成功, uk: %d", uk)
	return uk, sk, nil
}

// ClearSession 清除会话缓存
func (c *Client) ClearSession() {
	c.sessionMu.Lock()
	c.sk = ""
	c.uk = 0
	c.sessionAt = time.Time{}
	c.sessionMu.Unlock()
}
