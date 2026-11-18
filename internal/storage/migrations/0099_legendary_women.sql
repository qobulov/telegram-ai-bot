-- The best known women become Legendary, as the owners asked: of the 58
-- Legendary cards only Rem and Rei Ayanami were women, so twelve of AniList's
-- most loved women who were not Mythic join them, their numbers as the code
-- gives a Legendary (CardOf), their pictures AniList's. A card the catalog
-- already has keeps its name, anime and picture and takes the new rarity and
-- numbers; one it lacks is added. The players who hold one keep it, worth
-- 2000 points now, and have their points counted again.
INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value, collection) VALUES
    (137079, 'Power', 'Chainsaw Man', 'https://s4.anilist.co/file/anilistcdn/character/large/b137079-6yLEUYR3bmpr.png', 20439, 3, 3210, 13360, 2000, 'anime'),
    (120649, 'Kaguya Shinomiya', 'Kaguya-sama: Love is War', 'https://s4.anilist.co/file/anilistcdn/character/large/b120649-NPaWaIpWy60E.png', 20305, 3, 3760, 13430, 2000, 'anime'),
    (133676, 'Marin Kitagawa', 'My Dress-Up Darling', 'https://s4.anilist.co/file/anilistcdn/character/large/b133676-kV2czE3C8Qls.png', 17325, 3, 3630, 11050, 2000, 'anime'),
    (89361, 'Megumin', 'KONOSUBA -God''s blessing on this wonderful world!', 'https://s4.anilist.co/file/anilistcdn/character/large/b89361-tq8PQQ4MmF0M.png', 16532, 3, 3480, 14050, 2000, 'anime'),
    (61, 'Robin Nico', 'ONE PIECE', 'https://s4.anilist.co/file/anilistcdn/character/large/b61-ywXUyyocEEqt.png', 14890, 3, 2910, 14280, 2000, 'anime'),
    (94, 'Asuka Langley Souryuu', 'Neon Genesis Evangelion', 'https://s4.anilist.co/file/anilistcdn/character/large/b94-d631a3Z2KPvd.png', 14757, 3, 2660, 11760, 2000, 'anime'),
    (22037, 'Hitagi Senjougahara', 'Bakemonogatari', 'https://s4.anilist.co/file/anilistcdn/character/large/b22037-sY7GWSKYr2Nl.jpg', 14717, 3, 2960, 12010, 2000, 'anime'),
    (138102, 'Yor Forger', 'SPY x FAMILY', 'https://s4.anilist.co/file/anilistcdn/character/large/b138102-ZOAu9jI2d5ke.png', 14391, 3, 3870, 10000, 2000, 'anime'),
    (133700, 'Nobara Kugisaki', 'JUJUTSU KAISEN', 'https://s4.anilist.co/file/anilistcdn/character/large/b133700-f6sOO3TcgLV6.png', 13559, 3, 2590, 14140, 2000, 'anime'),
    (138100, 'Anya Forger', 'SPY x FAMILY', 'https://s4.anilist.co/file/anilistcdn/character/large/b138100-4Li0tWRCa5bQ.png', 13230, 3, 3130, 14430, 2000, 'anime'),
    (723, 'Nami', 'ONE PIECE', 'https://s4.anilist.co/file/anilistcdn/character/large/b723-vp5hPptgnNEC.png', 10230, 3, 3900, 9290, 2000, 'anime'),
    (36828, 'Asuna Yuuki', 'Sword Art Online', 'https://s4.anilist.co/file/anilistcdn/character/large/b36828-j5ib0adAzGMx.png', 10074, 3, 2680, 9450, 2000, 'anime')
ON CONFLICT (id) DO UPDATE SET rarity = EXCLUDED.rarity, attack = EXCLUDED.attack, health = EXCLUDED.health,
    value = EXCLUDED.value, updated_at = now();

UPDATE card_players pl SET points = coalesce((
    SELECT sum(c.value) FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = pl.user_id
), 0)
WHERE pl.user_id IN (SELECT user_id FROM card_owned
    WHERE card_id IN (137079, 120649, 133676, 89361, 61, 94, 22037, 138102, 133700, 138100, 723, 36828));
