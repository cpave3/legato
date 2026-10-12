-- The image files one revision of an agent UI item uses, as the service
-- encodes them: the name the content refers to an image by, and the stored
-- copy of the file. Empty for a revision with no images.
ALTER TABLE agent_ui_item_revisions ADD COLUMN images TEXT NOT NULL DEFAULT '';
