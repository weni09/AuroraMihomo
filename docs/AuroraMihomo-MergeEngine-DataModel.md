# Merge Engine Data Model

```go
type Config struct {
    Proxies []Proxy
    ProxyGroups []ProxyGroup
    Rules []Rule
    RuleProviders map[string]RuleProvider
    DNS DNSConfig
    TUN TUNConfig
}

type Conflict struct {
    ID string
    Type string
    Path string
    Local any
    Remote any
    Resolution string
}

type MergePolicy struct {
    ProxyPriority string
    RulePriority string
    DNSPriority string
}
```

Merge flow:

Base Config -> Remote Config -> Conflict Detection -> Policy -> Final Config
