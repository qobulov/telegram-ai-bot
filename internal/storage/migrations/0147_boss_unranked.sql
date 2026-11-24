-- Whether a boss fight counts on the week's tables and prizes, as the owners
-- asked on 2026-11-24: an owner's fights take no ticket, so they count for
-- neither, lest one owner's hundred fights top the total table.
ALTER TABLE boss_fights ADD COLUMN ranked boolean DEFAULT true NOT NULL;
