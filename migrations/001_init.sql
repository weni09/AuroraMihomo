CREATE TABLE subscriptions (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL,
 url TEXT NOT NULL,
 enabled INTEGER DEFAULT 1,
 last_update DATETIME
);

CREATE TABLE configs (
 id INTEGER PRIMARY KEY,
 type TEXT,
 content TEXT,
 created_at DATETIME
);

CREATE TABLE conflicts (
 id INTEGER PRIMARY KEY,
 type TEXT,
 path TEXT,
 resolution TEXT,
 resolved INTEGER DEFAULT 0
);
