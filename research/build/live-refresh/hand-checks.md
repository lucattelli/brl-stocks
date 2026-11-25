# Hand checks: live refresh (slice 3)

Gathered from increments 1–4 (PRs #12–#15). The stand-in setup is in `verification/README.md` (section "The view"); each step lists what to look for. None is recorded as done until you say so.

## 1. Time passes with a healthy source (#12)

1. Start the stand-in and `brl-stocks` against it with `refresh_seconds = 10`, as in the README.
2. Leave it open for a few minutes. Look for quote times that move with no `!` mark.
3. Switch B3 closed through the stand-in's `_control`, then open again. The requests stop and resume with no key pressed.
4. On the real source during B3 hours: leave the view open for a few minutes. The quote times advance.
5. Optional: leave the view open overnight on a real terminal. The next morning, the rows refresh after the open, and the cache folder has not grown (QR8).

## 2. Failures over time (#13)

1. With the README's view setup, switch the stand-in to refusing: `curl -X POST -d '{"status":429}' …/_control`.
2. Look for `source is rate-limiting` on the error line, with a next attempt 5 minutes ahead.
3. Switch it back with `'{}'`. The error line goes at the time it showed, with no key pressed.

## 3. `r` (#14)

1. Press `r`. The stand-in's PETR4 request count goes up by one.
2. Press `r` again at once. The count stays the same.
3. During a 429 block (as in 2.1), press `r`. The count stays the same, and the status line shows `next attempt HH:MM`.

## 4. The config file while the view is open (#15)

With `brl-stocks` open, edit `~/.config/brl-stocks/config.toml` in your own editor and watch the screen after each step:

1. Add a ticker. Its row appears within about a second.
2. Break a bracket. The rows stay, and the error line names the file and the problem.
3. Fix it. The error line goes.
4. `rm` the file. The rows stay, and the error line says the file was deleted.
5. Write it back. The error line goes.
