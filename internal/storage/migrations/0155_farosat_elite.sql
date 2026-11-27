-- Farosat's elite, as the owners asked on 2026-11-27: a member whose
-- farosat reaches 300 grams may give them up with /elita, and from then on
-- their farosat grows by 20 to 30 grams a day and shrinks by only 1 or 2.
ALTER TABLE farosat ADD COLUMN IF NOT EXISTS elite boolean NOT NULL DEFAULT false;
