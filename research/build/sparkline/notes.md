# Build notes: slice 4

What was awkward or new while building slice 4 with brl-factory 0.6.1, for the factory. Short entries.

- **The spec described the sparkline in prose with no criterion.** Section 7.3 gave its width, direction, colour, the narrow-terminal order and the regular session, and only S10.1–S10.3 tested anything. The scope found it by reading 7.3, not the story. `plan-slice` stage 1 could ask, for each surface, which behaviour sentences of the spec's "seen from outside" section have no criterion.
- **Two fresh reviews in a row each found something the other did not.** The scope check found the undecided shape (Q99–Q102). The review of the spec update found that QR8 cannot hold when the view starts early in a session (Q103), which no earlier slice saw, because their stand-ins served full series.
- **Resumed agents across slices worked.** The test writer, builder and verifier of slice 3 were resumed for slice 4 with about 150 words each, and kept their helpers.
- **Verification checks can depend on the hour.** The checks ran just after midnight UTC, and one assumed a close two hours earlier was today's. Another counted requests at a finer resolution than the stand-in recorded. Neither was the product's. A verifier brief could ask for every time comparison to be made at the stand-in's resolution and in the tool's zone.
- **Small increments are cheap to verify.** One increment of 21 criteria was one sitting for each agent. The whole slice, from scope to closing, took one evening.
