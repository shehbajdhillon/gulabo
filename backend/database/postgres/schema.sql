DROP TABLE IF EXISTS user_info CASCADE;
CREATE TABLE user_info (
  user_id BIGSERIAL PRIMARY KEY NOT NULL,
  telegram_user_id BIGINT UNIQUE NOT NULL,
  telegram_username TEXT,
  telegram_first_name TEXT,
  telegram_last_name TEXT,
  created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP TABLE IF EXISTS user_credits CASCADE;
CREATE TABLE user_credits (
  id BIGSERIAL PRIMARY KEY NOT NULL,
  user_id BIGINT REFERENCES user_info (user_id) ON DELETE CASCADE UNIQUE NOT NULL,
  credits_balance INT NOT NULL DEFAULT 20,
  created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_user_credits_user_id ON user_credits(user_id);

-- Simplified conversations with JSONB message history
DROP TABLE IF EXISTS conversations CASCADE;
CREATE TABLE conversations (
  id BIGSERIAL PRIMARY KEY NOT NULL,
  telegram_user_id BIGINT UNIQUE REFERENCES user_info (telegram_user_id) ON DELETE CASCADE NOT NULL,
  messages JSONB NOT NULL DEFAULT '[]'::jsonb,
  created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX idx_conversations_messages ON conversations USING gin (messages);
