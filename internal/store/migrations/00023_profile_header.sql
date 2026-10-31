-- +goose Up
-- The name is written garner now, also in the comment that starts each
-- profile's text, which exports and shares carry along. Only that header
-- changes, where the format puts it: on the first line, or on the line
-- after #:schema. The rest of the text, the user's own comments included,
-- stays as it is.
UPDATE profiles SET text = '# A garner profile;' || substr(text, 20)
WHERE substr(text, 1, 19) = '# A Garner profile;';
UPDATE profiles SET text =
    substr(text, 1, instr(text, char(10))) || '# A garner profile;' || substr(text, instr(text, char(10)) + 20)
WHERE substr(text, 1, 8) = '#:schema'
    AND substr(text, instr(text, char(10)) + 1, 19) = '# A Garner profile;';

-- +goose Down
UPDATE profiles SET text = '# A Garner profile;' || substr(text, 20)
WHERE substr(text, 1, 19) = '# A garner profile;';
UPDATE profiles SET text =
    substr(text, 1, instr(text, char(10))) || '# A Garner profile;' || substr(text, instr(text, char(10)) + 20)
WHERE substr(text, 1, 8) = '#:schema'
    AND substr(text, instr(text, char(10)) + 1, 19) = '# A garner profile;';
