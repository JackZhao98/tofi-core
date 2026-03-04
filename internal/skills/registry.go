package skills

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// RegistryClient 连接 skills.sh Agent Skills Registry
type RegistryClient struct {
	baseURL    string
	httpClient *http.Client
}

// RegistrySkill 代表 registry 中的一个技能概要
// 字段匹配 skills.sh/api/search 的实际返回格式
type RegistrySkill struct {
	ID          string `json:"id"`          // slug 标识符
	Name        string `json:"name"`        // 显示名称
	Description string `json:"description"` // 描述（可能为空）
	Source      string `json:"source"`      // 来源仓库 (owner/repo)
	Installs    int    `json:"installs"`    // 周安装量
	Stars       int    `json:"stars"`       // GitHub stars
}

// RegistrySearchResult 搜索结果
type RegistrySearchResult struct {
	Skills []RegistrySkill `json:"skills"`
	Total  int             `json:"total"`
}

// NewRegistryClient 创建 registry 客户端
// 默认使用 skills.sh，也支持自定义 registry（通过 SKILLS_API_URL 环境变量）
func NewRegistryClient(baseURL string) *RegistryClient {
	if baseURL == "" {
		baseURL = "https://skills.sh"
	}
	return &RegistryClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Search 搜索 registry 中的技能
// 对应 skills CLI 的 `npx skills find [query]`
// API: GET {baseURL}/api/search?q={query}&limit={limit}
func (c *RegistryClient) Search(query string, limit int) (*RegistrySearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	u := fmt.Sprintf("%s/api/search?q=%s&limit=%d",
		c.baseURL, url.QueryEscape(query), limit)

	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("registry search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var result RegistrySearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse registry response: %w", err)
	}

	return &result, nil
}

// GetSkillPage 获取 skills.sh 上技能详情页的 URL
func (c *RegistryClient) GetSkillPage(owner, repo, skillName string) string {
	return fmt.Sprintf("%s/%s/%s/%s", c.baseURL, owner, repo, skillName)
}
