# Your 99% eval gate can never pass

*2026-09-08*

Write this in your eval config:

```yaml
gates:
  tool_success: { min: 0.99, confidence: 0.95 }
budgets:
  max_samples_per_case: 5
```

It reads like a strict quality bar. It is not a bar at all. With three eval cases
at five samples each, that gate cannot pass, cannot fail, and will report
"inconclusive" on every run until someone changes the numbers. Airlock shipped
exactly this as its default, and its own toy demo could never go green.

## Where 381 comes from

A gate that fires on a point estimate is a coin flip. Nine successes out of nine
is 100%, and it is also what you would see fairly often from a system that fails
one request in twenty. So Airlock gates on the lower bound of a Wilson confidence
interval instead: pass only when the interval sits entirely above the threshold.

For a flawless run, where every sample succeeded, the Wilson lower bound has a
closed form:

```
low = n / (n + z²)
```

With 95% confidence, `z² ≈ 3.8416`. Note what is missing: the success rate. A
perfect run of 9 gives a lower bound of about 0.70 no matter how perfect it was.
Small samples cannot express high confidence. Set the threshold, solve for `n`:

```
n ≥ min · z² / (1 − min)
```

| Gate `min` | Clean samples needed |
|---|---|
| 0.80 | 16 |
| 0.90 | 35 |
| 0.95 | 73 |
| 0.99 | **381** |
| 0.995 | **765** |
| 0.999 | 3838 |

A 99% gate needs 381 consecutive successes before the statistics will let it say
yes. At 9 samples it is not strict. It is undefined, and it stays undefined
forever, because retrying does not add samples.

Worse, "inconclusive" tends to be configured as non-blocking, on the reasonable
theory that you should not fail a build over missing information. So the strictest
number in the config produces the weakest gate in practice: it never passes, never
fails, and never blocks anything. Airlock now prints the shortfall directly:

```console
│ tool_success      100.0%  [ 43.8%, 100.0%]  INCONCLUSIVE           │
│     CI [0.4385, 1.0000] straddles min 0.9900; a flawless run needs │
│     >= 381 samples to clear this min, have 3                       │
```

## And if it is not flawless

Suppose your agent genuinely succeeds 99% of the time and you set the gate to
0.99. More samples do not rescue you. They do not even convict you:

| Samples at an observed 99% | 95% interval | Verdict |
|---|---|---|
| 100 | [0.9455, 0.9982] | inconclusive |
| 1000 | [0.9817, 0.9946] | inconclusive |
| 3000 | [0.9858, 0.9930] | inconclusive |
| 100000 | [0.9894, 0.9906] | inconclusive |

The interval tightens around the true rate, and the true rate is the threshold, so
it closes in on 0.99 from both sides and keeps straddling it. At a hundred
thousand samples the gate is still undecided, and it always will be. A threshold
set at your actual performance is not a strict gate, it is a permanently open
question.

Headroom is what makes a gate decide. A system truly at 98.5% against that same
0.99 gate does fail, once you buy enough samples to see it:

| Samples at an observed 98.5% | 95% interval | Verdict |
|---|---|---|
| 400 | [0.9677, 0.9931] | inconclusive |
| 1000 | [0.9754, 0.9909] | inconclusive |
| 3000 | [0.9800, 0.9888] | **fail** |

Three thousand samples to detect that a system is half a point below its gate.
That is the real price of a threshold set close to the thing it measures.

## What to do instead

**Pick the min your sample budget can defend.** Divide the work backwards: decide
what you can afford to run on every pull request, read the required sample count
out of the table, and set the gate there. A defensible 0.90 gate beats an
aspirational 0.99 one that never resolves.

**Make undecided gates block.** If a gate cannot resolve, that is information
about your eval suite, not a reason to ship. In Airlock that is
`--fail-on-inconclusive`. Whatever your tooling, the default of "inconclusive
passes" is how a gate quietly stops being a gate.

**Sample where the variance is.** Sample count is per metric, not per suite. A
metric only exercised by one of your cases gets a fraction of the samples, and it
will be the one stuck inconclusive. Airlock's toy agent needed 16 samples per case
to give `tool_success` enough, because only one case used a tool.

**Separate the two questions.** "Is this good enough in absolute terms" needs
hundreds of samples. "Did this change make things worse" needs far fewer, because
a paired comparison against a baseline cancels most of the noise. Regression gates
resolve at sample counts where absolute gates cannot, so lean on them for
per-pull-request signal and run the absolute bar less often.

The numbers in the table are computed by
[`stats.SamplesToClearMin`](https://github.com/xdlc-labs/airlock/blob/main/internal/stats/stats.go),
and the tests assert each one is the true minimum: one fewer sample does not
clear the gate.
