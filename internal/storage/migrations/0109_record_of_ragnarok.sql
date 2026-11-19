-- Record of Ragnarok's fighters join the cards, as the owners asked on
-- 2026-11-19: Qin Shi Huang (Ying Zheng) as Mythic and thirteen more as
-- Legendary (Brunhilde, Sasaki Kojiro, Shiva, Hades, Poseidon, Beelzebub,
-- Thor, Heracles, Nikola Tesla, Lu Bu, Raiden Tameemon, Zeus, Leonidas), with
-- AniList's pictures and favourites and their rarity's numbers as the code
-- gives them (CardOf). Adam, Jack the Ripper and Buddha, already there, stay.
INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value, collection) VALUES
    (203633, 'Qin Shi Huang', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b203633-0ERteocAvO31.png', 917, 4, 5960, 19760, 4000, 'anime'),
    (140636, 'Brunhilde', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b140636-nuL1uHEuedtJ.png', 836, 3, 2730, 13340, 2000, 'anime'),
    (140646, 'Kojirou Sasaki', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b140646-j8tTUNjhFYPQ.png', 747, 3, 3910, 9830, 2000, 'anime'),
    (140650, 'Shiva', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b140650-6Dw43gfarW5U.jpg', 380, 3, 2730, 10250, 2000, 'anime'),
    (252553, 'Hades', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b252553-BpzOlTtycREr.png', 325, 3, 3660, 12830, 2000, 'anime'),
    (140641, 'Poseidon', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b140641-sL1cVxyjIA6h.png', 271, 3, 3250, 14010, 2000, 'anime'),
    (252554, 'Beelzebub', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b252554-qRDOqJURah0M.jpg', 269, 3, 3750, 12060, 2000, 'anime'),
    (140638, 'Thor', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b140638-OymphqBKzuDp.png', 245, 3, 2700, 14480, 2000, 'anime'),
    (156052, 'Heracles', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b156052-pcWYjkMx7Anu.png', 242, 3, 3780, 12050, 2000, 'anime'),
    (284632, 'Nikola Tesla', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b284632-DDu4NjQoqOOx.png', 239, 3, 3050, 14940, 2000, 'anime'),
    (140645, 'Lu Bu', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b140645-8VBxjc5X3Nzn.png', 185, 3, 2880, 10890, 2000, 'anime'),
    (168999, 'Raiden Tameemon', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b168999-8RUxExPuwk92.png', 177, 3, 3130, 11170, 2000, 'anime'),
    (140640, 'Zeus', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b140640-w78CFviA9DNm.png', 127, 3, 3740, 13180, 2000, 'anime'),
    (341135, 'Leonidas', 'Record of Ragnarok', 'https://s4.anilist.co/file/anilistcdn/character/large/b341135-pQqEFTwCDIRc.jpg', 40, 3, 3220, 9310, 2000, 'anime')
ON CONFLICT (id) DO UPDATE SET rarity = EXCLUDED.rarity, attack = EXCLUDED.attack, health = EXCLUDED.health,
    value = EXCLUDED.value, updated_at = now();
