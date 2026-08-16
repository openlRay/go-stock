package agent

import (
	"context"
	"errors"
	"fmt"
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

const maxMottoRunes = 2000

var (
	ErrMottoContentEmpty   = errors.New("格言内容不能为空")
	ErrMottoContentTooLong = errors.New("格言内容不能超过 2000 个字符")
	ErrMottoNotFound       = errors.New("格言不存在或已被删除")
)

type mottoModelFactory func(context.Context, data.AIConfig) (model.ToolCallingChatModel, error)

// MottoApi 负责格言持久化和只针对当前正文的 AI 润色。
type MottoApi struct {
	modelFactory mottoModelFactory
}

// NewMottoApi 创建格言服务。
func NewMottoApi() *MottoApi {
	return &MottoApi{modelFactory: createChatModel}
}

// MottoPolishRequest 描述一次不会自动持久化的格言润色请求。
type MottoPolishRequest struct {
	Content    string `json:"content"`
	AIConfigID int    `json:"aiConfigId"`
}

// MottoPolishResult 返回供用户继续编辑的草稿正文。
type MottoPolishResult struct {
	Content string `json:"content"`
}

// List 按最近更新时间和 ID 倒序返回格言。
func (a *MottoApi) List() ([]models.Motto, error) {
	mottos := make([]models.Motto, 0)
	if err := db.Dao.Order("updated_at DESC, id DESC").Find(&mottos).Error; err != nil {
		return nil, fmt.Errorf("读取格言列表失败: %w", err)
	}
	return mottos, nil
}

// Create 保存一条经过统一校验的格言。
func (a *MottoApi) Create(content string) (*models.Motto, error) {
	normalized, err := normalizeMottoContent(content)
	if err != nil {
		return nil, err
	}
	motto := &models.Motto{Content: normalized}
	if err := db.Dao.Create(motto).Error; err != nil {
		return nil, fmt.Errorf("创建格言失败: %w", err)
	}
	return motto, nil
}

// Update 更新指定格言，不存在的记录不会被静默创建。
func (a *MottoApi) Update(id uint, content string) (*models.Motto, error) {
	if id == 0 {
		return nil, ErrMottoNotFound
	}
	normalized, err := normalizeMottoContent(content)
	if err != nil {
		return nil, err
	}
	result := db.Dao.Model(&models.Motto{}).Where("id = ?", id).Update("content", normalized)
	if result.Error != nil {
		return nil, fmt.Errorf("更新格言失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrMottoNotFound
	}
	var motto models.Motto
	if err := db.Dao.First(&motto, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMottoNotFound
		}
		return nil, fmt.Errorf("读取更新后的格言失败: %w", err)
	}
	return &motto, nil
}

// Delete 删除指定格言，并区分不存在记录。
func (a *MottoApi) Delete(id uint) error {
	if id == 0 {
		return ErrMottoNotFound
	}
	result := db.Dao.Delete(&models.Motto{}, id)
	if result.Error != nil {
		return fmt.Errorf("删除格言失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrMottoNotFound
	}
	return nil
}

// Random 从格言库随机选择最多 limit 条，同一次查询不会重复。
func (a *MottoApi) Random(limit int) ([]models.Motto, error) {
	if limit <= 0 {
		return []models.Motto{}, nil
	}
	mottos := make([]models.Motto, 0, limit)
	if err := db.Dao.Order("RANDOM()").Limit(limit).Find(&mottos).Error; err != nil {
		return nil, fmt.Errorf("随机读取格言失败: %w", err)
	}
	return mottos, nil
}

// Polish 使用选中的对话模型润色当前正文，只返回草稿且不写数据库。
func (a *MottoApi) Polish(ctx context.Context, req MottoPolishRequest) (*MottoPolishResult, error) {
	content, err := normalizeMottoContent(req.Content)
	if err != nil {
		return nil, err
	}
	settings := data.GetSettingConfig()
	if settings == nil || len(settings.AiConfigs) == 0 {
		return nil, fmt.Errorf("尚未配置可用的 AI 模型")
	}
	if req.AIConfigID > 0 && !hasMottoChatConfig(settings.AiConfigs, req.AIConfigID) {
		return nil, fmt.Errorf("所选 AI 配置不存在或已被删除")
	}
	aiConfig, ok := data.ResolveAIConfig(settings.AiConfigs, req.AIConfigID)
	if !ok || aiConfig == nil {
		if req.AIConfigID > 0 {
			return nil, fmt.Errorf("所选 AI 配置不存在或已被删除")
		}
		return nil, fmt.Errorf("尚未设置默认 AI 模型，请先前往 AI 模型服务配置")
	}

	// thinking 是请求级能力，必须作用于配置副本，不能污染设置缓存。
	requestConfig := data.WithSessionThinkingOverride(*aiConfig, false)
	chatModel, err := a.modelFactory(ctx, requestConfig)
	if err != nil {
		return nil, fmt.Errorf("无法初始化 AI 模型，请检查模型配置")
	}
	response, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage("你是格言文字润色助手。只润色用户提供的当前格言，保留核心含义和事实，不扩写为文章，不调用任何工具或外部数据源。只输出一段纯文本，不要 Markdown、引号、标题、解释或前后缀。"),
		schema.UserMessage(content),
	})
	if err != nil {
		return nil, fmt.Errorf("AI 润色失败，请稍后重试")
	}
	if response == nil {
		return nil, fmt.Errorf("AI 未返回可用的润色内容")
	}
	polished := normalizePolishedMotto(response.Content)
	if polished == "" {
		return nil, fmt.Errorf("AI 未返回可用的润色内容")
	}
	if utf8.RuneCountInString(polished) > maxMottoRunes {
		return nil, fmt.Errorf("AI 返回内容过长，请缩短原文后重试")
	}
	return &MottoPolishResult{Content: polished}, nil
}

func hasMottoChatConfig(configs []*data.AIConfig, id int) bool {
	for _, config := range configs {
		if config == nil || int(config.ID) != id {
			continue
		}
		modelType := strings.ToLower(strings.TrimSpace(config.ModelType))
		return modelType == "" || modelType == "chat"
	}
	return false
}

func normalizeMottoContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ErrMottoContentEmpty
	}
	if utf8.RuneCountInString(content) > maxMottoRunes {
		return "", ErrMottoContentTooLong
	}
	return content, nil
}

func normalizePolishedMotto(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") && strings.HasSuffix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(strings.TrimPrefix(content, "text"))
	}
	// 产品契约要求单段纯文本；压平模型偶发的换行，保留用户可继续编辑的内容。
	return strings.Join(strings.Fields(content), " ")
}
