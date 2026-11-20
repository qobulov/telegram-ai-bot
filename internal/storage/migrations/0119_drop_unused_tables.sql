-- Drop the tables nothing uses any more, as the owners asked on 2026-11-20:
-- image_generations counted each user's pictures a day for generate_image,
-- which was taken out (its code is kept under the git tag
-- archive/image-generation); group_notes held the saved notes of a feature
-- that is long gone, and nothing has read or written it since.
DROP TABLE IF EXISTS image_generations;
DROP TABLE IF EXISTS group_notes;
