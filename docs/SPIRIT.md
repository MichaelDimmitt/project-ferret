# Spirit

The thing to read when a decision feels load-bearing and the design docs
don't settle it. Everything else in `docs/` describes what was built. This
describes what it was *for*, so that drift is visible.

It is short on purpose. A statement of intent that takes twenty minutes to
read is a specification, and specifications get argued with instead of
consulted.

---

## The one sentence

**An aid, not an authority: something you drop on a machine that tells you
what is wrong in seconds, and asks before it does anything about it.**

---

## The prompt this grew out of

The whole posture compresses into four instructions. This is the artifact to
look back at:

> 1. **Make it run.** Explore until you can build, test, and execute the
>    project yourself. Report what you actually found, not what the README
>    claims.
> 2. **Name the best tool, not the nearest one.** If it isn't installed, stop
>    and ask me to install it. Never silently downgrade to a worse tool that
>    happens to be present.
> 3. **Surface, don't swallow.** Blockers, ambiguity, and surprises come to
>    me. You are not scored on appearing self-sufficient.
> 4. **Don't read what isn't yours.** Keys, credentials, `.env`, personal
>    data, customer dumps: don't open them, don't pull them into context,
>    don't echo them. If one sits in your path, say so and stop there.

Compressed further, for a place where four numbered points won't fit:

> Get it running. Use the right tool, not the one that's here — ask me to
> install it. Surface problems instead of working around them. Never open or
> echo secrets; stop and tell me.

**Both forms are kept, and that is deliberate.** The long form is the
standing instruction — it says *why* each rule exists, and a rule whose
reason is visible survives contact with a situation its author didn't
foresee. The short form is what fits in a system prompt, a skill
description, or a README header, where the long form would be cut down by
whoever needed the space, badly and without the reasoning.

They are not alternatives to choose between. The short form is a pointer to
the long one. If they ever disagree, the long form is right and the short
one needs rewriting.

---

## Why these four, and not others

Each rule exists because its opposite is the *comfortable* failure — the one
a capable agent slides into while appearing to do good work.

**Make it run** is first because everything else is speculation without it. A
README describes intent; a repo has state. The gap between them is precisely
where the interesting problems live, and an agent that reports the README
back has told you nothing you couldn't have read yourself.

**Name the best tool** is the anti-drift rule. Silently downgrading to
what's installed is *locally reasonable every single time* and globally
disastrous: it converts an explicit "you need pnpm" into an unexamined npm
install with lockfile drift, and nobody ever decided that. The cost of asking
is one interruption. The cost of not asking is a machine that diverges from
every other machine, invisibly.

**Surface, don't swallow** names the incentive directly — *you are not
scored on appearing self-sufficient* — because that is the pressure that
produces the failure. An agent optimizing to look competent hides exactly
the information that makes it useful.

**Don't read what isn't yours** is absolute rather than careful, because
"be careful with secrets" degrades under pressure and "never open it" does
not. A value never read cannot leak from a log, a crash dump, a scrollback
buffer, or a debug print added at 2am.

---

## What this implies, concretely

**Seconds, not minutes.** The unit of value is a fast honest answer. Anything
that makes the first run slower needs to earn it, and heavyweight
prerequisites are suspect by default.

**Silence where there is nothing to say.** A tool that speaks only when
something is wrong gets read. A tool that always speaks gets filtered, and a
filtered tool has failed completely rather than partially.

**A false positive is worse than a missing check.** Crying wolf is total
failure. This is why an untested check is not shipped, and why "we chose not
to check that" must be written down somewhere a reader can find it —
otherwise it is indistinguishable from having forgotten.

**Aid, not authority.** It reports and proposes. It does not decide. Every
mutation is gated on a human saying yes to a specific list they can see and
edit.

**The AI is the flexible half.** The deterministic part should stay small and
boring — measure, report, stop. Judgment, synthesis, and remediation planning
belong to a model reading that output, not to more code in the tool. Code
that tries to be smart about a broken machine is code that will be confidently
wrong on a machine its author never saw.

---

## The tests this document exists to apply

Ask these when a decision is unclear. They are phrased so the uncomfortable
answer is the informative one.

1. **Does this make the first run slower or heavier?** If yes, what does it
   buy, and would a user trade a fresh dependency for it?
2. **Would a user in a hurry read this output?** If it only pays off for
   someone studying it, it is built for the wrong reader.
3. **Is this something code must do, or something a model reading the output
   could do better?** Prefer the model. It is the part that can adapt.
4. **If this check is wrong, how would anyone find out?** A check that cannot
   be caught being wrong should not ship.
5. **Am I building the thing, or the framework for the thing?** The second is
   a comfortable place to hide.

---

## How this was already drifted from, on the record

Stated because a spirit document with no admitted failures is decoration.

The tool was specified as *an aid that answers in seconds*. What got built
first, and built well, was a 3,100-line Go engine with a JSON manifest
schema, a validation layer, a taint DAG, and a redaction subsystem — around
7,800 lines total including tests. It is genuinely good code with a real
defect record behind it. It also requires a **261 MB Go toolchain** to do a
job — run a command, compare a string — that shell already does: the shell
fallback answers 8 Tier-1 checks in ~0.13s, the compiled engine answers 34 in
~0.9s, and a cold first run compiling the binary takes ~1.8s. Both are far
below the threshold where a human notices. The toolchain was never buying
speed that mattered.

Nothing about that was a bad decision in isolation. Each step followed from
the last, every milestone met its exit criteria, and the invariants held. The
drift is only visible against intent: an aid became an engine, because
building an engine is a well-defined, satisfying problem and "make it useful
in seconds" is not.

That is the failure mode this document exists to make visible early. See
`docs/REASSESSMENT.md` for what follows from it.

---

## What this document is not

It is not a spec — `docs/design/MANIFEST_SCHEMA.md` is the contract, and it
wins on any question of field semantics. It is not a plan —
`docs/plan/PLAN.md` orders the work. It is not a list of invariants —
`AGENTS.md` binds behavior.

It is the thing those three are *for*. When one of them says to do something
that fails the tests above, that is worth stopping over.
