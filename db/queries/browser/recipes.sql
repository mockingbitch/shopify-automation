-- name: InsertRecipe :one
INSERT INTO browser_recipes (id, capability_key, market, version, recipe_json, notes, created_by)
VALUES (@id, @capability_key, @market, @version, @recipe_json, @notes, @created_by)
RETURNING *;

-- name: ListRecipes :many
SELECT * FROM browser_recipes ORDER BY capability_key, market, version DESC;

-- name: GetRecipe :one
SELECT * FROM browser_recipes WHERE id = @id;

-- name: LockRecipe :one
SELECT * FROM browser_recipes WHERE id = @id FOR UPDATE;

-- name: NextRecipeVersion :one
SELECT COALESCE(max(version), 0)::int + 1 FROM browser_recipes WHERE capability_key = @capability_key AND market = @market;

-- name: SetRecipeState :one
UPDATE browser_recipes SET enabled = @enabled, disabled_reason = sqlc.narg(disabled_reason), verified_at = sqlc.narg(verified_at),
  verified_by = sqlc.narg(verified_by), notes = @notes
WHERE id = @id
RETURNING *;

-- name: DisableOtherVersions :exec
UPDATE browser_recipes SET enabled = false, disabled_reason = 'replaced by version ' || @version::int
WHERE capability_key = @capability_key AND market = @market AND id <> @id AND enabled;

-- name: EnabledRecipe :one
SELECT * FROM browser_recipes WHERE capability_key = @capability_key AND market = @market AND enabled;

-- name: EnabledRecipeKeys :many
SELECT capability_key FROM browser_recipes WHERE market = @market AND enabled;

-- name: RecordRecipeSuccess :exec
UPDATE browser_recipes SET success_count = success_count + 1, consecutive_failures = 0 WHERE id = @id;

-- name: RecordRecipeFailure :one
UPDATE browser_recipes SET failure_count = failure_count + 1, consecutive_failures = consecutive_failures + 1,
  enabled = CASE WHEN consecutive_failures + 1 >= @auto_disable_after::int THEN false ELSE enabled END,
  disabled_reason = CASE WHEN enabled AND consecutive_failures + 1 >= @auto_disable_after::int
    THEN 'Disabled automatically after ' || @auto_disable_after::int || ' failures in a row' ELSE disabled_reason END
WHERE id = @id
RETURNING enabled, consecutive_failures;
