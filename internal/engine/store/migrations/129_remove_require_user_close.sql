-- swarm.require_user_close never did anything and was removed from the
-- registry; a value saved for it is deleted.
DELETE FROM settings WHERE key = 'swarm.require_user_close';
