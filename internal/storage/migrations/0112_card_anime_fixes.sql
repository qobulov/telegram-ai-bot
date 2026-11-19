-- Cards named after a show their character only passes through, as the
-- owners found on 2026-11-19: AniList gave each card its character's most
-- popular anime, cameos included, so Saiki and Kuroko stood under
-- Assassination Classroom and Holo under Eromanga Sensei. The catalog now
-- passes over cameos (aniListCharacter.anime); these are the cards whose
-- anime was another series altogether. A season of the same series, which
-- the Animelar tab groups with it, is left as it is.
UPDATE card_catalog c SET anime = v.anime, updated_at = now()
FROM (VALUES
    (1742, 'Detective Conan'),
    (3662, 'Baccano!'),
    (3796, 'Baccano!'),
    (4835, 'Urusei Yatsura'),
    (7156, 'Hatsune Miku: Colorful Stage!'),
    (7373, 'Spice and Wolf'),
    (18769, 'Kuroko''s Basketball'),
    (31008, 'Oreimo'),
    (36309, 'Hunter x Hunter (2011)'),
    (51523, 'Fate/EXTRA Last Encore'),
    (55977, 'Fate/Apocrypha'),
    (89644, 'NEW GAME!'),
    (90107, 'The Disastrous Life of Saiki K.'),
    (354440, 'The Ramparts of Ice')
) AS v(id, anime)
WHERE c.id = v.id;
