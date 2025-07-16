-------------------- UserInfo Queries --------------------

-- name: AddUser :one
INSERT INTO user_info (email, full_name) VALUES ($1, $2) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM user_info WHERE email = $1 LIMIT 1;

-- name: GetUserById :one
SELECT * FROM user_info WHERE user_id = $1 LIMIT 1;

-- name: DeleteUserById :exec
DELETE FROM user_info WHERE user_id = $1;

-- name: UpdateOnboardingStatus :exec
UPDATE user_info SET onboarding_complete = $1 WHERE user_id = $2;

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

