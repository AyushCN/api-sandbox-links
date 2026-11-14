ALTER TABLE sandboxes
    DROP COLUMN IF EXISTS services_override,
    DROP COLUMN IF EXISTS start_command;
