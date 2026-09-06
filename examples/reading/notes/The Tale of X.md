# The Tale of X

- finished:: 2026-01-18
- status:: done
- author:: Alice Napier
- genre:: novel
- pages:: 320
- rating:: 8

## Notes

A reading note. Each `key:: value` line is an inline field (ADR 0032): indexed as a typed property
(`props.author`, `props.pages`, …) *and* read as the sentence it is, so the body renders whole.
`pages` and `rating` are typed `number`, `finished` a `date`, and `status` is checked against the
`unread|reading|done` enum by the `properties:` schema in config.yml.
