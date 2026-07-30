ALTER TABLE subscriptions ADD COLUMN user_agent TEXT;
ALTER TABLE subscriptions ADD COLUMN cached_nodes TEXT;

CREATE TABLE IF NOT EXISTS sub_files (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  content TEXT,
  sync_url TEXT,
  created_at DATETIME,
  updated_at DATETIME
);
