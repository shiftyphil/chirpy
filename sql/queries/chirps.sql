-- name: CreateChirp :one
INSERT INTO chirps (id, created_at, updated_at, body, user_id)
VALUES (gen_random_uuid(),
        NOW(),
        NOW(),
        $1,
        $2)
RETURNING *;

-- name: GetChirps :many
SELECT id, created_at, updated_at, body, user_id
FROM chirps
ORDER BY
    CASE WHEN NOT @reverse::boolean THEN created_at END ASC,
    CASE WHEN @reverse::boolean THEN created_at END DESC;

-- name: GetChirpsByAuthor :many
SELECT id, created_at, updated_at, body, user_id
FROM chirps
WHERE user_id = $1
ORDER BY
    CASE WHEN NOT @reverse::boolean THEN created_at END ASC,
    CASE WHEN @reverse::boolean THEN created_at END DESC;

-- name: GetChirp :one
SELECT id, created_at, updated_at, body, user_id
FROM chirps
WHERE id = $1;

-- name: DeleteChirpWithUser :execrows
DELETE FROM chirps
WHERE id = $1
AND user_id = $2;;
