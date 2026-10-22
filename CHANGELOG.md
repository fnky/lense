# Changelog

All notable changes to Bitwarden Mini are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## 0.1.0 - Unreleased

The first version: Quick Access for the Bitwarden desktop app, in the macOS
menu bar. It reads the vault the desktop app keeps on the Mac and makes no
requests to Bitwarden's servers.

### Added

#### Unlocking

- Unlock with Touch ID through the Bitwarden desktop app, over its local
  socket, as Bitwarden's browser extensions do. Unlock with Touch ID opens
  Bitwarden, out of the way, when it isn't running, and waits for it to answer.
- Unlock with Touch ID of this Mac after Mini locked itself: the Secure Enclave
  seals the account's key until Mini quits.
- Unlock with the master password, offline, with PBKDF2 or Argon2id, as the
  account uses.
- Touch ID is asked for as you show the window with its shortcut or the menu
  bar, not as the app opens.
- Lock with ⌘L, from the menu bar, after a time without input (10 minutes
  by default), and as the Mac sleeps or its screen locks.
- Items that ask for the master password again ask before they show, copy or
  fill a secret.
- First launch sets up the account: its email and server (bitwarden.com,
  bitwarden.eu, or your own), checked against the accounts signed in to
  Bitwarden, with a way to fix a mismatch.

#### Searching

- Search logins, cards, notes, identities and SSH keys by name, username and
  website.
- Filters, completed as you type `=`: `=favorite`, `=tag:`, `=untagged`,
  `=category:`, `=vault:`, `=passkey` and `=location:`.
- Suggestions for the page your browser shows, in Safari and Chromium
  browsers.
- A vault selector (⌘K, or ⌘1 to ⌘9) for All Vaults, My Vault, organizations,
  folders and collections.
- The vault is read again as Bitwarden syncs it.

#### Copying and filling

- Return fills the login into the browser you came from, or the app with a
  login field focused, or else copies the password.
- Shift-Return opens the login's website in the browser and fills it.
- Copy the username (⌘C or ⌘B), the password (⇧⌘C) and the one-time password
  (⌥⌘C) from anywhere in Quick Access.
- The item's fields: Return copies one and closes the window; a click copies
  it and keeps the window open; a drag drops it into a field of another app.
- One-time passwords with a countdown ring.
- Reveal passwords with → or by holding ⌥, and show any field in Large Type.
- Copied passwords and codes are marked concealed for clipboard managers, and
  cleared after 90 seconds, unless you copied something else.
- Open an item in a window of its own (⌘O), or in Bitwarden (⇧⌘O).

#### The window

- ⇧⌘Space shows Quick Access from any app, without taking the focus from it,
  on the display the pointer is on, where you left it there.
- Opening the app shows the window, with a spinner, while it reads
  Bitwarden's data.
- Guides that the window snaps to as you move it, as Raycast's do.
- macOS's materials behind the content: Popover by default, or Solid, Menu,
  HUD, Sidebar or Under Window.
- The list and the details scroll under the footer, which macOS's own
  material frosts, and stretch past their ends, as AppKit's scroll views do.
- Section headers stay at the top of the list, with the rows fading out under
  them.
- Tooltips name the buttons of an icon, unless you turn them off.

#### Settings

- Every key of Quick Access can be changed, its shortcut included.
- Idle lock, lock on sleep, Touch ID, screen capture, window material,
  tooltips, and how soon copied secrets clear.

### Security

- Only what lists and searches the vault is decrypted: passwords, codes, card
  numbers, notes, keys and hidden fields stay encrypted in memory until they
  show, copy or fill.
- The vault's keys stay encrypted in memory, in memguard's enclaves, which
  open them for each decryption alone. Locking forgets the vault.
- The answers from Bitwarden that carry the account's key are wiped after use.
- The master password fields turn on Secure Event Input, as macOS's own do.
- Debuggers can't attach (set `BWMINI_DEBUG=1` to allow them), and core dumps
  are off.
- Screenshots and screen recordings leave Mini's windows out, unless you allow
  them in Settings.
- Builds signed with a certificate run under the hardened runtime.
