CREATE TABLE IF NOT EXISTS conflicts (
  id INTEGER PRIMARY KEY,
  key TEXT,
  type TEXT,
  path TEXT,
  local_value TEXT,
  remote_value TEXT,
  manual_value TEXT,
  resolution TEXT,
  resolved INTEGER DEFAULT 0,
  created_at DATETIME,
  updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS config_versions (
  id INTEGER PRIMARY KEY,
  hash TEXT,
  content TEXT,
  file_path TEXT,
  note TEXT,
  created_at DATETIME
);

CREATE TABLE IF NOT EXISTS sub_collections (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER DEFAULT 1,
  template TEXT DEFAULT 'mihomo-yaml',
  share_token TEXT,
  created_at DATETIME,
  updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS sub_collection_items (
  id INTEGER PRIMARY KEY,
  collection_id INTEGER NOT NULL,
  subscription_id INTEGER NOT NULL,
  priority INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sub_rules (
  id INTEGER PRIMARY KEY,
  name TEXT,
  scope TEXT DEFAULT 'name',
  pattern TEXT,
  replace TEXT,
  filter_mode TEXT DEFAULT 'rewrite',
  enabled INTEGER DEFAULT 1,
  priority INTEGER DEFAULT 0,
  created_at DATETIME,
  updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS sub_templates (
  id INTEGER PRIMARY KEY,
  name TEXT,
  type TEXT DEFAULT 'mihomo-yaml',
  content TEXT,
  created_at DATETIME,
  updated_at DATETIME
);
