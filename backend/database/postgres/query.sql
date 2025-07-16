-------------------- UserInfo Queries --------------------

-- name: AddUser :one
INSERT INTO user_info (telegram_user_id, telegram_username, telegram_first_name, telegram_last_name) VALUES ($1, $2, $3, $4) RETURNING *;


-- name: GetUserByTelegramUserId :one
SELECT * FROM user_info WHERE telegram_user_id = $1 LIMIT 1;

-- name: DeleteUserByTelegramUserId :exec
DELETE FROM user_info WHERE telegram_user_id = $1;

-------------------- User Credits Queries --------------------

-- name: CreateUserCredits :one
INSERT INTO user_credits (user_id, credits_balance) VALUES ($1, 20) RETURNING *;

-- name: GetUserCreditsByUserID :one
SELECT * FROM user_credits WHERE user_id = $1 LIMIT 1;

-- name: AddUserCredits :one
UPDATE user_credits
SET credits_balance = credits_balance + sqlc.arg(amount), updated = CURRENT_TIMESTAMP
WHERE user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DecrementUserCredits :one
UPDATE user_credits
SET credits_balance = credits_balance - 1, updated = CURRENT_TIMESTAMP
WHERE user_id = $1
RETURNING *;

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