-- The Seven Deadly Sins: Four Knights of the Apocalypse, which the players
-- asked for and AniList's 1500 most loved left out: its four knights and
-- Arthur as Epic, Percival's friends as Rare, their numbers as the code
-- gives them (CardOf), their pictures AniList's.
INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value, collection) VALUES
    (206212, 'Percival', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b206212-EyBnNcsSdqMx.png', 163, 2, 2020, 6290, 400, 'anime'),
    (265209, 'Lancelot', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b265209-NAHBUq0SX3Vb.png', 262, 2, 1900, 8070, 400, 'anime'),
    (249281, 'Tristan Liones', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b249281-2UxCTm6D6NAa.png', 119, 2, 1760, 6460, 400, 'anime'),
    (282753, 'Gawain', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b282753-gJ5R4nrEVQFJ.png', 48, 2, 2290, 8730, 400, 'anime'),
    (122210, 'Arthur Pendragon', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b122210-l6g1EMCf5NdE.png', 558, 2, 2010, 7440, 400, 'anime'),
    (247947, 'Nasiens', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b247947-7KpURs8nZNbT.png', 55, 1, 1060, 3430, 200, 'anime'),
    (247949, 'Anghalhad', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b247949-8O2zw4YYJJwm.png', 37, 1, 1500, 3930, 200, 'anime'),
    (247948, 'Donny', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b247948-j95hDCFSuAIm.png', 20, 1, 1160, 4110, 200, 'anime')

ON CONFLICT (id) DO NOTHING;
