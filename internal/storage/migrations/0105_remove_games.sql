-- The games the owners removed on 2026-11-19: of the bot's games only Dev
-- jangi (boss), O'tinchi (otinchi), Mafia (mafiya) and Kartalar are left in
-- @BotFather. Tarozi's and Kosmik ilon's scores go; Shashka's players,
-- games and queue go with their tables. Dooz kept nothing in the database.
DELETE FROM game_scores WHERE game IN ('tarozi', 'ilon');
DROP TABLE IF EXISTS shashka_queue, shashka_players, shashka_games;
