package parser

import "testing"

func TestParseShareSS(t *testing.T) {
	// method:pass = aes-128-gcm:pwd => YWVzLTEyOC1nY206cHdk
	nodes, err := ParseShareLinks("ss://YWVzLTEyOC1nY206cHdk@1.2.3.4:443#NodeA", "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Type != "ss" || nodes[0].Server != "1.2.3.4" {
		t.Fatalf("%+v", nodes)
	}
}

func TestParseSIP008(t *testing.T) {
	raw := []byte(`{"version":1,"servers":[{"server":"9.9.9.9","server_port":8388,"method":"aes-256-gcm","password":"x","remarks":"S1"}]}`)
	nodes, err := ParseSIP008(raw, "t")
	if err != nil || len(nodes) != 1 || nodes[0].Name != "S1" {
		t.Fatalf("err=%v nodes=%+v", err, nodes)
	}
}

func TestParseSurge(t *testing.T) {
	raw := "ProxySS = ss, 8.8.8.8, 443, encrypt-method=aes-128-gcm, password=pwd"
	nodes, err := ParseSurge(raw, "t")
	if err != nil || len(nodes) != 1 || nodes[0].Type != "ss" {
		t.Fatalf("err=%v nodes=%+v", err, nodes)
	}
}
