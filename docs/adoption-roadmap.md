# Metro adoption roadmap: first 90 days

**Status:** proposed plan, starting when the maintainer accepts it. No audience,
usage, or contributor numbers are claimed here. Metro core is on `main`;
Metro Web is still an experimental stack of draft PRs, with [stage 006](https://github.com/safal207/Liminal-Rail-Metro/pull/39)
as a runnable preview. The first job is to make its current behavior easy to
try and honestly evaluate, not to announce a new Internet as already built.

## Positioning

For developers building agent workflows that need to navigate changing
resources and permitted actions, Metro is an experimental **route and evidence
layer**: a bounded graph transition, stable action identity, and an observable
receipt. The current preview demonstrates a local menu graph and read-only,
version-pinned file delivery. It does not yet provide arbitrary website
rendering, general-purpose discovery, secure identity for remote publishers,
or production authorization.

This positioning is an architectural proposal, not a compatibility claim:

| Existing protocol | What it already standardizes | Metro's proposed adjacent question |
| --- | --- | --- |
| [MCP](https://modelcontextprotocol.io/specification/2025-11-25/architecture) | Client/server access to tools, resources, and prompts | How does an agent choose a bounded path through changing resource states and check the resulting action? |
| [A2A](https://a2a-protocol.org/v1.0.0/specification/) | Communication, capability discovery, and task collaboration between agent systems | What route/evidence semantics can a local or delegated step carry without treating a message as execution proof? |

Build adapters only after a small interop experiment defines the mapping,
security model, and tests. Do not describe Metro as a replacement for MCP,
A2A, HTTPS, a browser, or an agent framework.

## Milestones and gates

| When | Deliverable | Evidence to pass the gate |
| --- | --- | --- |
| Days 0–14 | Confirm a root `LICENSE` is present on `main` (tracked in [PR #32](https://github.com/safal207/Liminal-Rail-Metro/pull/32)); merge this onboarding documentation and issue templates; assign one maintainer to answer reports. | License file is present on `main`, README license statement matches it, and a fresh clone follows the published commands. If the file is absent in a revision, do not present MIT as applying to that revision. |
| Days 0–30 | Bring the Metro Web draft stack onto one tested integration path; keep the read-only demo's route, file, and permission boundaries visible. | Stage PRs have passing relevant Linux/Windows checks, reviewed claim limits, a working clean-clone command, and an integration PR or ordered merge plan. Draft PR #39 alone is not a release. |
| Days 15–45 | Complete the independent Docker-based Developer Quickstart test in [issue #31](https://github.com/safal207/Liminal-Rail-Metro/issues/31) at its specified commit. Separately invite a clean-clone test of the Metro Web preview branch. Do not substitute one test for the other. | Issue #31 has one unrelated tester's report in its required format; a separate Metro Web report records OS, Go version, exact commands, observed result, and any undocumented step. Fix blockers and repeat the affected test. |
| Days 30–60 | Publish a usable preview only after the preceding gates: one scenario, a short screen capture or diagram, protocol limits, and a pinned source revision. | Someone new can run the demo, reach a verified menu result or full-file check, explain what the receipt proves, and report a failure through an issue template. |
| Days 45–90 | Test one real adopter problem and one narrow MCP *or* A2A adapter sketch. Invite critique of the graph/receipt model. | A public issue captures the adopter's goal, current workaround, expected route/evidence, security boundary, and a small executable acceptance test. Interop claims require a working test, not a diagram. |
| Day 90 | Decide whether to expand, revise, or stop the Metro Web track. | Review the measurements below and unresolved safety/UX reports. Publish the decision and the next three concrete issues. |

## Discovery and support loop

Use the repository as the canonical entry point: a plain-language README,
clean-clone demo, focused docs, useful issue labels, and a short preview video
or GIF that links back to the exact revision. Once licensing and preview gates
pass, add accurate GitHub topics and write one technical walkthrough of a
concrete task (“find an item under a budget without ordering”) that shows the
graph, denied actions, and receipt. Share it in developer communities that
permit project posts; answer questions there and invite counterexamples. Do
not automate unsolicited messages, inflate stars, fabricate testimonials, or
describe local benchmark figures as real-world agent throughput.

Support is part of adoption: acknowledge reproducible issues, label them by
stage and boundary, publish the next action, and close them with a test or a
documented limitation. Make first contributions tractable through small
docs/tests/fixture issues before asking others to change the protocol core.

## Measurements (start with a blank baseline)

Record these weekly in one public issue or project note, with dates and source
links. GitHub traffic and opt-in issue reports are enough for the first cycle;
the preview should not add user tracking just to measure adoption.

| Signal | Definition | What it tells us |
| --- | --- | --- |
| Discovery | Repository visitors/referrers where available, plus visits to the preview instructions; record the GitHub-reported time window. | Whether the entry point is being found. A view or star is not usage. |
| Activation | Unique opt-in clean-clone reports that reached a verified menu result or whole-file check, divided by all complete clean-clone attempts reported that week. | Whether a newcomer can actually use the preview. Record raw numerator and denominator; do not infer a population rate from a tiny sample. |
| Usability | Median time from clone to first verified result in volunteered reports, and the top three blockers. | Whether setup or explanation is the limiting factor. |
| Contribution | Distinct external people who opened a reproducible issue, review, or PR; accepted PRs tracked separately. | Whether there is engagement beyond the maintainer. Do not count bot comments as human support. |
| Reliability | Relevant CI pass rate by OS and unresolved reproducible bugs in the preview. | Whether public invitations are premature. |

Set numerical goals after the first two measured weeks. A sensible first gate
is one genuinely independent clean-room success and no known blocker in the
documented path; wider outreach waits for that evidence. If reach rises but
activation does not, fix onboarding or product behavior before posting more.
