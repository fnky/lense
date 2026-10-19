# Bitwarden Mini

A companion to the [Bitwarden](https://bitwarden.com) desktop app in the macOS
menu bar, in the manner of 1Password's Quick Access: a shortcut opens it from
any app to search the vault, copy from it, fill logins into other apps, and
drag usernames and passwords into their fields. Built with
[MyGo](https://github.com/egoist/mygo).

Bitwarden Mini makes no requests to Bitwarden's servers. It reads the vault
the desktop app keeps on the Mac (`data.json`, which holds it encrypted), and
decrypts it with:

- **Touch ID, through the desktop app.** Mini asks Bitwarden over its local
  socket, as Bitwarden's browser extensions do. Bitwarden shows its Touch ID
  prompt and hands back the account's key.
- **Touch ID of this Mac,** after Mini locked itself, until it quits. Locked,
  Mini holds nothing of the vault but its key, which the Mac's Secure Enclave
  seals: only Touch ID opens it, and the vault is decrypted again then.
- **The master password,** which derives the key offline (PBKDF2 or
  Argon2id, as the account uses).

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
go tool mygo build
```

The result is in `build/darwin-arm64/`. Before you distribute it, replace
the placeholder bundle identifier in `mygo.json`.

## Use it

On first launch, enter the email of your Bitwarden account and choose its
server: bitwarden.com, bitwarden.eu, or the address of your own. Mini checks
both against the accounts signed in to the desktop app, and says how to fix a
mismatch, such as an account that lives on the other region.

The keys act where Quick Access is: on the search results, on the fields
of the item chosen, or on the vaults. Change any of them in Settings, the
shortcut that shows Bitwarden Mini included.

| Search results | |
|---|---|
| ↑ ↓ | Choose an item |
| → | Go to the item's fields (at the end of the search) |
| ↩ | Fill the login into the browser you came from, or the app with a login field focused; else copy the password. On a filter, finish it |
| ⇥ | Finish the filter chosen |

| Item fields | |
|---|---|
| ↑ ↓ | Choose a field |
| ↩ | Copy the field |
| → | Reveal a password, then show it in Large Type; show other fields in Large Type |
| ← | Back to the results |

| Vault selector (⌘K) | |
|---|---|
| ↑ ↓ | Choose a vault, folder or collection |
| → ← | Show or hide what a vault holds; ← at the top goes back |
| ↩ | Search it |

| Anywhere | |
|---|---|
| ⇧⌘Space | Show Bitwarden Mini from any app, on the display the pointer is on, as you left it |
| ⌘C, ⌘B | Copy the username (a card's number, an identity's email, a note) |
| ⇧⌘C | Copy the password (a card's security code, a key's private half) |
| ⌥⌘C | Copy the one-time password |
| ⇧↩ | Open the login's website in the browser, and fill it |
| ⌘1 … ⌘9 | Search All Vaults, My Vault, an organization, then folders and collections |
| ⌘O | Open the item in a window of its own |
| ⇧⌘O | Open Bitwarden |
| ⌘F | Back to the search |
| ⌘L | Lock |
| ⌘, | Settings |
| esc | Close the selector or Large Type, clear the search, then close Bitwarden Mini, forgetting the passwords revealed |
| ⌘W | Close Bitwarden Mini |

Quick Access shows one of macOS's materials behind its content: Popover,
unless you choose Solid, Menu, HUD, Sidebar or Under Window in Settings →
Appearance. The list and the details scroll under the footer, which macOS's
own material frosts, as under Raycast's action bar. Buttons of an icon name
themselves in a tooltip, unless you turn tooltips off there too.

Hold ⌥ to see passwords. Click a field to copy it, or drag it into a field of
another app. The button of a website opens it, and fills the login in. Copied passwords and codes are marked concealed for clipboard
managers, and cleared after 90 seconds unless you copied something else.

### Search filters

| Filter | Finds |
|---|---|
| `=favorite` | favorites |
| `=tag:Work` | items in the folder or collection Work |
| `=untagged` | items in no folder or collection |
| `=category:card` | logins, cards, notes, identities or SSH keys |
| `=vault:"My Vault"` | items of a vault: My Vault or an organization's |
| `=passkey` | logins with a passkey |
| `=location:github.com` | logins for a website |

Filters combine with words and with each other, and complete as you type `=`.

### Permissions

- **Accessibility**, to fill other apps: Mini types into the field with the
  keyboard focus, and only into text fields of one line.
- **Automation** of your browser, to read the page it shows, for suggestions.
  Safari and Chromium browsers tell it; Firefox doesn't.

## Limits

- Items are added and changed in Bitwarden: its desktop app offers no local way
  to save them, and Mini doesn't go to Bitwarden's servers itself.
- Accounts using Bitwarden's newer encryption (COSE, "V2") can't be read yet.
