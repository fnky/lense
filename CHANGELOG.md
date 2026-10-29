# Changelog

All notable changes to Lense are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## 0.3.0 - 2026-10-29

### Added

- ↩ on a login opens its website in your default browser and fills it, when
  the page you came from isn't one of its websites; a login of several
  websites lets you choose one first, with ↑ ↓ and ↩, or esc to close the
  menu. ⇧↩ opens and fills a website even when you're on one of them already,
  and the footer says so as you hold ⇧.
- Cards and identities fill the page you came from, as Bitwarden's browser
  extension fills them: a card's name, number, expiration, as the page's
  field shows it, and security code, and an identity's name, address, state,
  country and the rest, choosing in the page's menus too. Where the page has
  no such fields, ↩ copies the card's number or the identity's email.
- Settings › Filling › Default URI match detection: how a login's websites
  match the page you're on where they don't say, as in Bitwarden: Base domain
  (recommended), Host, Starts with, Exact or Never. Lense suggests the logins
  it matches, and fills no other page. Base domain knows the domains of the
  Public Suffix List, as Bitwarden does: one site on github.io isn't another.
- Settings › Filling › Copy the one-time password: after filling a login that
  has one, Lense copies its one-time password, for the page that asks for it
  next. On by default.
- Settings › General › Launch Lense at login.
- Settings › Appearance › Window mode: Compact shows the search alone until
  you type, and with Show favorites in compact mode, your favorites under it.
- Typing = suggests =folder: and =collection: with the other filters, and then
  the names of your folders and collections.
- Scrolled past its end, a list's scroll bar shrinks as the content stretches,
  as macOS's does.

### Changed

- Lense fills a page's login form as Bitwarden's browser extension does: it
  finds the username, password and one-time password fields by their type,
  label and placeholder, a box for each digit of a code too, and sets each at
  once rather than typing it. It fills only a page with a login form, on one of
  the login's websites, never a form in a frame of the page, as another site's
  is, and only while the browser is in front, on the page the fill began on.
- Settings › Keyboard shortcuts records a key in a popover by the key you
  clicked, or by +: it shows the modifiers as you hold them, the new key in
  green once it's set, or why a key can't be one, and the key keeps what it
  had until then.
- The footer leaves out the keys to an item's fields and back.

## 0.2.1 - 2026-10-27

### Fixed

- Settings show the version of Lense that runs: 0.2.0 said it was 0.1.0, in
  Software Update and at the bottom of Settings.

## 0.2.0 - 2026-10-27

### Added

- Settings › Software Update: Check for Updates…, or Automatic updates, which
  check once a day and download an update, to install as Lense restarts. They
  are off until you turn them on: Lense goes online for nothing else, and then
  only to its releases on GitHub. Lense 0.1.0 doesn't update itself: install
  this version by hand.

### Fixed

- Lense no longer starts macOS's AutoFill helper as it opens, which could use
  CPU and make scrolling stall.

### Changed

- The downloads are named `lense-0.2.0-arm64.dmg`, for Apple silicon, and
  `lense-0.2.0.dmg`, universal, for Intel Macs too.

## 0.1.0 - 2026-10-26

The first version: Quick Access for the Bitwarden® desktop app, in the macOS
menu bar. It reads the vault the desktop app keeps on the Mac and makes no
requests to Bitwarden's servers. Free and open source, under the MIT license.
Signed with a Developer ID and notarized, for Apple silicon, and as a
universal app for Intel Macs too.

### Added

#### Unlocking

- Unlock with Touch ID through the Bitwarden desktop app, over its local
  socket, as Bitwarden's browser extensions do. Unlock with Touch ID opens
  Bitwarden, out of the way, when it isn't running, and waits for it to answer.
- Unlock with Touch ID of this Mac after Lense locked itself: the Secure Enclave
  seals the account's key until Lense quits.
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
- Acknowledgements of Bitwarden, and of MyGo, with its license.

### Security

- Only what lists and searches the vault is decrypted: passwords, codes, card
  numbers, notes, keys and hidden fields stay encrypted in memory until they
  show, copy or fill.
- The vault's keys stay encrypted in memory, in memguard's enclaves, which
  open them for each decryption alone. Locking forgets the vault.
- The answers from Bitwarden that carry the account's key are wiped after use.
- The master password fields turn on Secure Event Input, as macOS's own do.
- Debuggers can't attach (set `LENSE_DEBUG=1` to allow them), and core dumps
  are off.
- Screenshots and screen recordings leave Lense's windows out, unless you allow
  them in Settings.
- Builds signed with a certificate run under the hardened runtime.
