package substore

import (
	"context"
	"strings"
	"testing"
)

func TestPipelineExecution(t *testing.T) {
	// Raw input mimicking a V2Ray JSON subscription
	rawJson := `[{"protocol":"vmess","tag":"test_node_1","settings":{"vnext":[{"address":"1.2.3.4","port":8080,"users":[{"id":"uuid"}]}]}}]`

	e := NewEngine()

	// Define a pipeline with multiple operators
	ops := []PipelineOperator{
		{
			Type:    OpSetProperty,
			Enabled: true,
			Payload: map[string]interface{}{"udp": true, "tls": false},
		},
		{
			Type:    OpRename,
			Enabled: true,
			Payload: map[string]interface{}{"pattern": "test_node_1", "replace": "PremiumNode"},
		},
		{
			Type:    OpFlag,
			Enabled: true,
			Payload: map[string]interface{}{},
		},
		{
			Type:    OpScript,
			Enabled: true,
			Payload: map[string]interface{}{
				"script": `function operator(proxies) { 
					for (let i = 0; i < proxies.length; i++) { 
						proxies[i].name = "🇺🇸 " + proxies[i].name; 
						proxies[i].port = 443;
						proxies[i].tls = true; // Override what SetProperty did
					} 
					return proxies; 
				}`,
			},
		},
	}

	res, err := e.Convert(context.Background(), ConvertRequest{Content: rawJson}, nil, ops, "mihomo-yaml", "")
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if len(res.Nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(res.Nodes))
	}

	node := res.Nodes[0]
	// Check if Regex Rename and then JS Script modified the name correctly
	if !strings.Contains(node.Name, "🇺🇸 PremiumNode") {
		t.Errorf("Node name incorrect: %s", node.Name)
	}

	// Check if JS Script mutated the port
	if node.Port != 443 {
		t.Errorf("Node port incorrect, expected 443 got %d", node.Port)
	}

	// Check if SetProperty mutated UDP
	if !node.UDP {
		t.Errorf("UDP should be true from SetProperty")
	}

	// Check if JS Script overrode the SetProperty TLS setting
	if tls, ok := node.Extra["tls"].(bool); !ok || !tls {
		t.Errorf("TLS should be true from JS Script override")
	}
}
