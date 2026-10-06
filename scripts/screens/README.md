# Stock screens

`space.py` generates the stock space-themed screens in `configs/screens`:
the main menu, its submenus (messages, files, community), the sysop menu,
the logoff screen, and the lists and views of message areas, messages,
netmail, file areas and files, the door list, the banners of the community features and the welcome
screen —
each as `name.ans` (English text) and
`name.de.ans` (`{T:key}` placeholders, filled in for German and Du);
parts without text of their own (list rows) come as `name.ans` only.

```sh
python3 scripts/screens/space.py            # writes configs/screens
python3 scripts/screens/space.py /tmp/out/  # or somewhere else
```

`preview` draws a screen the way an 80-column terminal shows it, as a PNG:

```sh
go run ./scripts/screens/preview -lang de-du -sl 255 configs/screens/main.de.ans main.png
```

`-sl` below 200 shows the screen as a caller without the sysop's lines.

Rules for the art:

- At most 79 columns: the board lays screens out one column short of the
  terminal, since many clients wrap a line that fills the last column.
  `TestMenuScreensFit` checks this.
- Borders and text whose width changes with the language use `{FILL:x}`.
- `{SYSOP_ONLY}` in a line shows it from SL 200 only.
