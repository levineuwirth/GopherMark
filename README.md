# GopherMark

A terminal-based bookmark manager for Firefox/LibreWolf.

## Usage

```bash
# Find available profiles
gophermark -find

# Set database path (saved to config)
gophermark -db /path/to/places.sqlite

# Launch with saved config
gophermark
```

## Arguments

- `-db <path>` - Specify Firefox/LibreWolf places.sqlite database path
- `-find` - List all available browser profiles

## Keybindings

### Navigation
- `j/k` - Move up/down
- `Tab` - Switch between folders (left) and bookmarks (right) panes
- `Space` or `Enter` - Expand/collapse folders

### Editing
- `e` - Edit selected bookmark (title/URL)
- `n` - Add new bookmark
- `s` - Quick add to Scratch (unsorted links to refine later)
- `S` - Jump to Scratch folder
- `Esc` - Exit Scratch folder (navigate to Bookmarks Bar)
- `b` - Bulk move selected items (only in Scratch folder)
- `m` - Toggle selection for batch operations
- `d` - Delete selected bookmark(s)

### Advanced Features
- `i` - Toggle inspector panel (shows bookmark metadata)
- `a` - Audit links (check for dead/broken URLs)
- `D` - Detect duplicate bookmarks

### Other
- `/` - Search bookmarks (fuzzy match on title/URL)
- `x` - Export bookmarks (j=JSON, h=HTML)
- `Ctrl+S` - Commit changes (requires browser to be closed)
- `q` or `Ctrl+C` - Quit

## Notes

- Changes are made to a staging copy and committed atomically
- Browser must be closed before committing changes
- Config stored in `~/.config/gophermark/config.json`
