-------------------- UserInfo Queries --------------------

-- name: AddUser :one
INSERT INTO user_info (telegram_user_id, telegram_username, telegram_first_name, telegram_last_name) VALUES ($1, $2, $3, $4) RETURNING *;


-- name: GetUserByTelegramUserId :one
SELECT * FROM user_info WHERE telegram_user_id = $1 LIMIT 1;

-- name: DeleteUserByTelegramUserId :exec
DELETE FROM user_info WHERE telegram_user_id = $1;

-------------------- Subscription Plan Queries --------------------

-- name: IncrementSubscriptionResourcesUsedByTeamId :one
UPDATE subscription_plan SET resources_used = resources_used + $1 WHERE user_id = $2 RETURNING *;

-- name: CreateSubscription :one
INSERT INTO subscription_plan
(user_id, stripe_subscription_id, resources_included)
VALUES ($1, $2, $3) RETURNING *;

-- name: GetSubscriptionByTeamId :one
SELECT * FROM subscription_plan WHERE user_id = $1 ORDER BY created LIMIT 1;

-- name: GetSubscriptionByTeamIdSubscriptionId :one
SELECT * FROM subscription_plan WHERE user_id = $1 AND id = $2 LIMIT 1;

-- name: GetSubscriptionById :one
SELECT * FROM subscription_plan WHERE id = $1 LIMIT 1;

-- name: GetSubscriptionByStripeSubscriptionId :one
SELECT * FROM subscription_plan WHERE stripe_subscription_id = $1 LIMIT 1;

-- name: SetSubscriptionStripeIdByTeamId :one
UPDATE subscription_plan SET stripe_subscription_id = $2 WHERE user_id = $1 RETURNING *;

-- name: DeleteSubscriptionByStripeSubscriptionId :one
DELETE FROM subscription_plan WHERE stripe_subscription_id = $1 RETURNING *;

-- name: ResetSubscriptionResourcesUsed :one
UPDATE subscription_plan
SET resources_used = 0
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