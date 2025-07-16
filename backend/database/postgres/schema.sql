DROP TABLE IF EXISTS user_info CASCADE;
CREATE TABLE user_info (
  user_id BIGSERIAL PRIMARY KEY NOT NULL,
  telegram_user_id BIGINT UNIQUE NOT NULL,
  telegram_username TEXT,
  telegram_first_name TEXT,
  telegram_last_name TEXT,
  created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP TABLE IF EXISTS subscription_plan CASCADE;
CREATE TABLE subscription_plan (
  id BIGSERIAL PRIMARY KEY NOT NULL,
  user_id BIGINT REFERENCES user_info (user_id) ON DELETE CASCADE UNIQUE NOT NULL,
  stripe_subscription_id TEXT UNIQUE,
  resources_included INT NOT NULL DEFAULT 0,
  resources_used INT NOT NULL DEFAULT 0,
  created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Simplified conversations with JSONB message history
DROP TABLE IF EXISTS conversations CASCADE;
CREATE TABLE conversations (
  id BIGSERIAL PRIMARY KEY NOT NULL,
  telegram_user_id BIGINT REFERENCES user_info (telegram_user_id) ON DELETE CASCADE NOT NULL,
  messages JSONB NOT NULL DEFAULT '[]'::jsonb,
  created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX idx_conversations_telegram_user_id ON conversations (telegram_user_id);
CREATE INDEX idx_conversations_messages ON conversations USING gin (messages);

