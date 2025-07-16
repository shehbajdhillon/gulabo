DROP TABLE IF EXISTS user_info CASCADE;
CREATE TABLE user_info (
  user_id BIGSERIAL PRIMARY KEY NOT NULL,
  email TEXT UNIQUE NOT NULL,
  full_name TEXT NOT NULL,
  onboarding_complete BOOLEAN DEFAULT false NOT NULL,
  created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP TABLE IF EXISTS subscription_plan CASCADE;
CREATE TABLE subscription_plan (
  id BIGSERIAL PRIMARY KEY NOT NULL,
  user_id BIGINT REFERENCES user_info (user_id) ON DELETE CASCADE UNIQUE NOT NULL,
  stripe_subscription_id TEXT UNIQUE,
  resources_included INT NOT NULL DEFAULT 0,     -- Included resources per plan
  resources_used INT NOT NULL DEFAULT 0,                 -- Tracks total resources used
  created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

