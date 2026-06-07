<!-- track-template
name: example
-->
# {{ title }}

<!--
Example track template. It exercises every option the current template
implementation supports, so it doubles as a quick reference. Use it either way:

  - Copy this file into your vault's `template/` directory and rename it to
    `<id>.template.md`, where `<id>` is a number (e.g. `1700000000000.template.md`);
    `track template list` only recognizes numeric template filenames.
  - Or run `track template new --name <name>` to create an empty template, then
    paste this body into the file it opens.

See docs/spec/templates.md for the full reference. You can delete this comment
block (and the placeholder sections below) once you have copied the template.

Directive  (the block between "<!-- track-template" and "-->"):
  name:  Required. The stable template name used by `--template` and
         `:Track from_template`. Any other line in the directive is ignored,
         so lines like this one are free-form notes.

Substitutions  use the {{ name }} form (double braces) with one of the names
below; they are replaced once, when the note is created. See the body under this
comment for live examples.
  title  Target note title (the journal name, e.g. 20260607, for journals).
  id     The new note id.
  date   Creation date in track's date format (currently YYYY-MM-DD).
  kind   Either "note" or "journal".

Rendering rule: the rendered body must contain an H1 heading. For a regular note
that H1 must equal the requested title, which the H1 line above already does.
Substitutions are plain text replacement only; there is no executable substitution.
-->

- Created: {{ date }}
- Id: {{ id }}
- Kind: {{ kind }}

## Notes

## Links

A template body can contain any track markup. Wiki links such as [[Some Note]]
and action links work normally once the note is created, for example:

[Open today's journal](<journal?offset=0>)
