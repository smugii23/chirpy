-- name: AddRefreshToken :exec
INSERT INTO refresh_tokens (
    token,
    created_at,
    updated_at,
    user_id,
    expires_at,
    revoked_at
)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetRefreshToken :one
SELECT user_id 
FROM refresh_tokens
WHERE token = $1
AND expires_at > NOW()
AND (revoked_at IS NULL OR revoked_at > NOW());