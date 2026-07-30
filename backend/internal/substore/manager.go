package substore

import (
	"context"
)

// Manager keeps backward-compatible interface used by service layer.
type Manager interface {
	FetchAndConvert(ctx context.Context, subscriptionURL string) ([]byte, error)
}

type ManagerAdapter struct {
	engine *Engine
	rules  []RewriteRule
}

func NewManager(_ Config) *ManagerAdapter {
	// Config retained for compatibility; Go engine does not need node runtime.
	return &ManagerAdapter{engine: NewEngine()}
}

func NewManagerWithRules(rules []RewriteRule) *ManagerAdapter {
	return &ManagerAdapter{engine: NewEngine(), rules: rules}
}

func (m *ManagerAdapter) FetchAndConvert(ctx context.Context, subscriptionURL string) ([]byte, error) {
	res, err := m.engine.Convert(ctx, ConvertRequest{URL: subscriptionURL}, m.rules, nil, "mihomo-yaml", "")
	if err != nil {
		return nil, err
	}
	return []byte(res.YAML), nil
}

// legacy config placeholder
type Config struct {
	NodePath       string
	SubStoreScript string
	WorkingDir     string
}
