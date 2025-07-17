-------------------- UserInfo Queries --------------------

-- name: AddUser :one
INSERT INTO user_info (telegram_user_id, telegram_username, telegram_first_name, telegram_last_name) VALUES ($1, $2, $3, $4) RETURNING *;


-- name: GetUserByTelegramUserId :one
SELECT * FROM user_info WHERE telegram_user_id = $1 LIMIT 1;

-- name: DeleteUserByTelegramUserId :exec
DELETE FROM user_info WHERE telegram_user_id = $1;

-------------------- User Credits Queries --------------------

-- name: CreateUserCredits :one
INSERT INTO user_credits (user_id, credits_balance) VALUES ($1, 10) RETURNING *;

-- name: GetUserCreditsByUserID :one
SELECT * FROM user_credits WHERE user_id = $1 LIMIT 1;

-- name: GetUserCreditsByTelegramUserId :one
SELECT uc.credits_balance FROM user_credits uc JOIN user_info ui ON uc.user_id = ui.user_id WHERE ui.telegram_user_id = $1;

-- name: AddUserCreditsByTelegramUserId :one
UPDATE user_credits
SET credits_balance = credits_balance + sqlc.arg(amount), updated = CURRENT_TIMESTAMP
FROM user_info
WHERE user_credits.user_id = user_info.user_id AND user_info.telegram_user_id = sqlc.arg(telegram_user_id)
RETURNING user_credits.*;

-- name: DecrementUserCreditsByTelegramUserId :one
UPDATE user_credits
SET credits_balance = credits_balance - 1, updated = CURRENT_TIMESTAMP
FROM user_info
WHERE user_credits.user_id = user_info.user_id AND user_info.telegram_user_id = $1 AND user_credits.credits_balance > 0
RETURNING user_credits.*;

-------------------- Conversation Queries --------------------

-- name: CreateConversation :one
INSERT INTO conversations (telegram_user_id, messages)
VALUES ($1, '[]'::jsonb) RETURNING *;

-- name: GetConversationByTelegramUserId :one
SELECT * FROM conversations WHERE telegram_user_id = $1 LIMIT 1;

-- name: UpdateConversationMessages :one
UPDATE conversations 
SET messages = $2, updated = CURRENT_TIMESTAMP 
WHERE telegram_user_id = $1 
RETURNING *;

-- name: ClearConversationMessages :one
UPDATE conversations
SET messages = '[]'::jsonb, updated = CURRENT_TIMESTAMP
WHERE telegram_user_id = $1
RETURNING *;
