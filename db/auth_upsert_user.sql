-- auth_upsert_user — SECURITY DEFINER function that bypasses RLS for the
-- OAuth flow. Called during Google callback before a tenant user_id is known.
-- The function runs as the table owner (superuser at migration time), so RLS
-- is not evaluated.

CREATE OR REPLACE FUNCTION auth_upsert_user(
  p_email     text,
  p_google_id text,
  p_name      text DEFAULT NULL
) RETURNS users AS $$
DECLARE
  v_user users;
BEGIN
  INSERT INTO users (email, google_id)
  VALUES (p_email, p_google_id)
  ON CONFLICT (google_id) DO UPDATE
    SET email = EXCLUDED.email
  RETURNING * INTO v_user;

  RETURN v_user;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;
