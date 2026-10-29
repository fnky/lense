# Lense

A companion to the [Bitwarden®](https://bitwarden.com) desktop app in the
macOS menu bar, in the manner of 1Password's Quick Access: a shortcut opens it
from any app to search the vault, copy from it, fill logins into other apps,
and drag usernames and passwords into their fields. Free and open source,
under the [MIT license](LICENSE). Built with
[MyGo](https://github.com/egoist/mygo).

Lense is an independent project, not affiliated with or endorsed by
Bitwarden, Inc.

Lense makes no requests to Bitwarden's servers, and goes online for nothing
but its own updates, from its releases on GitHub: when you click Check for
Updates… in Settings › Software Update, or once a day with Automatic updates
on, which they are not until you turn them on. An update installs only with
the signature of Lense's releases. Lense reads the vault the desktop app keeps
on the Mac (`data.json`, which holds it encrypted), and decrypts it with:

- **Touch ID, through the desktop app.** Lense asks Bitwarden over its local
  socket, as Bitwarden's browser extensions do. Bitwarden shows its Touch ID
  prompt and hands back the account's key.
- **Touch ID of this Mac,** after Lense locked itself, until it quits. Locked,
  Lense holds nothing of the vault but its key, which the Mac's Secure Enclave
  seals: only Touch ID opens it, and the vault is decrypted again then.
- **The master password,** which derives the key offline (PBKDF2 or
  Argon2id, as the account uses).

Unlocked, Lense decrypts only what lists and searches the vault: names,
usernames, websites, a card's last digits. Passwords, codes, card numbers,
notes, keys and hidden fields stay encrypted in memory, as Bitwarden stores
them, until they show, copy or fill; the vault's keys stay encrypted in
memory too, in [memguard](https://github.com/awnumar/memguard)'s enclaves,
which open them for each decryption alone.

## Requirements

- macOS 14 or newer, with the Bitwarden desktop app signed in to your account
- For Touch ID: in Bitwarden's Settings, turn on **Unlock with Touch ID** and
  **Allow browser integration**
- Go 1.27 or newer, to build it

## Run locally

```sh
go tool mygo dev
```

This builds a development bundle, launches it, and rebuilds it as the code
changes. Build the app and a disk image with:

```sh
go tool mygo build -sign "Apple Development" -skip-notarize
```

The result is in `build/darwin-arm64/`. Signed with a certificate of yours,
such as the Apple Development one Xcode makes, each build stays the same app
to macOS, which keeps the Accessibility permission it gave the last; signed ad
hoc (`-sign -`), each build is a new app, and the switch in System Settings
stays on but no longer applies.

### Release

[RELEASING.md](RELEASING.md) says how a release is made: built, signed and
notarized on GitHub into a draft release, then published.

## Use it

On first launch, enter the email of your Bitwarden account and choose its
server: bitwarden.com, bitwarden.eu, or the address of your own. Lense checks
both against the accounts signed in to the desktop app, and says how to fix a
mismatch, such as an account that lives on the other region.

Opening the app shows Quick Access, whether it was running or not; it waits
with its buttons off while it reads the desktop app's data. Locked, it asks
for Touch ID as you show it with its shortcut or the menu bar, not as the app
opens. Opened at login, it stays in the menu bar.

The keys act where Quick Access is: on the search results, on the fields
of the item chosen, or on the vaults. Change any of them in Settings, the
shortcut that shows Lense included.

| Search results | |
|---|---|
| ↑ ↓ | Choose an item |
| → | Go to the item's fields (at the end of the search) |
| ↩ | Fill the login into the page you came from, when it is one of its websites, or the app it is for; else open its website and fill it, or choose one of several. A login without a website fills the browser you came from, or the app with a login field focused, or else copies the password. On a filter, finish it |
| ⇥ | Finish the filter chosen |

| Item fields | |
|---|---|
| ↑ ↓ | Choose a field |
| ↩ | Copy the field, and close |
| → | Reveal a password, then show it in Large Type; show other fields in Large Type |
| ← | Back to the results |

| Vault selector (⌘K) | |
|---|---|
| ↑ ↓ | Choose a vault, folder or collection |
| → ← | Show or hide what a vault holds; ← at the top goes back |
| ↩ | Search it |

| Open and Fill menu (↩, ⇧↩, for a login of several websites) | |
|---|---|
| ↑ ↓ | Choose a website of the login, or the app you came from |
| ↩ | Open the website in your default browser and fill it, or fill the app |
| esc | Close the menu |

| Anywhere | |
|---|---|
| ⇧⌘Space | Show Lense from any app, on the display the pointer is on, as you left it |
| ⌘C, ⌘B | Copy the username (a card's number, an identity's email, a note) |
| ⇧⌘C | Copy the password (a card's security code, a key's private half) |
| ⌥⌘C | Copy the one-time password |
| ⇧↩ | Open the login's website in your default browser, or choose one of several, and fill it, even on one of them already; hold ⇧ to see it in the footer |
| ⌘1 … ⌘9 | Search All Vaults, My Vault, an organization, then folders and collections |
| ⌘O | Open the item in a window of its own |
| ⇧⌘O | Open Bitwarden |
| ⌘F | Back to the search |
| ⌘L | Lock |
| ⌘, | Settings |
| esc | Close a menu, the selector or Large Type, clear the search, then close Lense, forgetting the passwords revealed |
| ⌘W | Close Lense |

Quick Access shows one of macOS's materials behind its content: Popover,
unless you choose Solid, Menu, HUD, Sidebar or Under Window in Settings →
Appearance. The list and the details scroll under the footer, which macOS's
own material frosts, as under Raycast's action bar. Buttons of an icon name
themselves in a tooltip, unless you turn tooltips off there too.

Hold ⌥ to see passwords. Click a field to copy it, which keeps Quick Access
open, unlike ↩, or drag it into a field of another app. The button of a website opens it, and fills the login in. Copied passwords and codes are marked concealed for clipboard
managers, and cleared after 90 seconds unless you copied something else.

A login's websites match a page by their own URI match detection, or where
they don't say, by Settings → Filling → Default URI match detection: Base
domain, unless you choose Host, Starts with, Exact or Never. Lense suggests
the logins of the page you're on by it, and fills no page it doesn't match.
After filling a login, or finding nothing to fill, Lense copies its one-time
password, for the page that asks for it next, unless you turn that off
there. As Bitwarden's, it never puts the password on the clipboard itself.

### Search filters

| Filter | Finds |
|---|---|
| `=favorite` | favorites |
| `=tag:Work` | items in the folder or collection Work |
| `=folder:Work` | items in the folder Work |
| `=collection:Engineering` | items in the collection Engineering |
| `=untagged` | items in no folder or collection |
| `=category:card` | logins, cards, notes, identities or SSH keys |
| `=vault:"My Vault"` | items of a vault: My Vault or an organization's |
| `=passkey` | logins with a passkey |
| `=location:github.com` | logins for a website |

Filters combine with words and with each other, and complete as you type `=`.

### Permissions

- **Accessibility**, to fill other apps. A browser's page Lense fills as
  Bitwarden's browser extension does: it finds the login form's fields by
  their type, identifier, label and placeholder, as the extension does by
  their HTML; the username's, the password's, every one of a form that signs
  up, and the one-time password's, a box for each digit too; leaves out
  search, captcha, disabled, readonly and hidden fields, and those of a
  newsletter; and sets each value at once, as the page's input and change
  events tell it, then puts the focus on the password field. It fills only
  the pages of the login's websites, checked as it fills each field, never a
  frame of the page, and only the browser in front. An app's own fields,
  which tell less, Lense types into: the one with the keyboard focus, then,
  with Tab, the password's. It checks that each field took what it was
  given before it goes on to the next.
- **Automation** of your browser, to read the page it shows, for suggestions.
  Safari and Chromium browsers tell it; Firefox doesn't.

## Security

- Unlocked, only what lists and searches the vault is decrypted (see above);
  locked, Lense holds nothing of it, but the key Touch ID opens, sealed by the
  Secure Enclave.
- Debuggers can't attach to Lense: it denies them as it starts. Set
  `LENSE_DEBUG=1` to debug it.
- Core dumps are off, and the master password fields turn on Secure Event
  Input, as macOS's own do, so that other apps don't see what you type.
- Screenshots and screen recordings leave Lense's windows out, as Bitwarden's
  do, unless you allow screen capture in Settings → Security.
- Builds signed with a certificate (`-sign`, or `macos.signingIdentity` in
  `mygo.json`) run under the hardened runtime, which keeps debuggers, injected libraries
  and other processes out of Lense's memory; `entitlements.plist` lets it ask
  your browser for its page. Builds signed ad hoc don't, unless you sign them
  so yourself:

  ```sh
  codesign --force --deep --options runtime --entitlements entitlements.plist \
    --sign - "build/darwin-arm64/Lense.app"
  ```

## Limits

- Items are added and changed in Bitwarden: its desktop app offers no local way
  to save them, and Lense doesn't go to Bitwarden's servers itself.
- Accounts using Bitwarden's newer encryption (COSE, "V2") can't be read yet.

## License

Lense is released under the [MIT license](LICENSE). The libraries it is built
with keep their own licenses: MyGo (MIT), memguard and purego (Apache 2.0),
and Go's `x/crypto` and `x/sys` (BSD). Settings → Acknowledgements thanks
Bitwarden and MyGo, and carries MyGo's license, from `licenses/mygo.txt`.

Bitwarden is a trademark or registered trademark of Bitwarden, Inc. in the
United States and/or other countries.
