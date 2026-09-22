Status: done

# 01: web: the product mark on the auth screens

- Move `Mark` out of `app-shell.tsx` into `web/src/components/mark.tsx`,
  sized by className so the auth screen can draw it larger.
- `auth-screen.tsx` draws `Mark` in place of lucide's `Activity`.
- Verify the login screen in both Modes.

## Comments

- Typechecked; the auth screen is only reachable signed out, so its look is to be
  checked with the owner at the end of the branch.
