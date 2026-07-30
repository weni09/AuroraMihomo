package parser

type Node struct {
	Name   string
	Type   string
	Server string
	Port   int
	UDP    bool
	Extra  map[string]interface{}
	Source string
}

func newNode(name, typ, server string, port int, source string) Node {
	return Node{
		Name:   name,
		Type:   typ,
		Server: server,
		Port:   port,
		Extra:  map[string]interface{}{},
		Source: source,
	}
}
