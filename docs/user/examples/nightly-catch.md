# What a Nightly Run Caught

On 18 September 2026 the nightly run for [aat-shippo](https://github.com/gburgyan/aat-shippo) went red. One plan of twenty-eight; the other twenty-seven passed. Nobody was watching, and nothing on our side had changed for two days.

Working out what had happened took downloading one file and running two commands. No access to the Shippo account, no re-running anything against the live API, no adding logging and waiting for tomorrow's run. This page is that sequence, because it is the clearest example of what the [archives](../archives.md) are for.

Shippo have since confirmed it: a known defect in their **test** environment. Live refunds are unaffected, and were never in question — everything below happens against a test-mode API key. The interesting part is not the defect. It is that a scheduled run found it within a day, and that answering "what exactly changed, and when?" cost one download.

## The signal

The batch reports which plan failed and stops being interesting immediately after:

```
labels/buy-and-refund   FAILED
27 passed, 1 failed
```

That is all CI has to say, and it is the point at which a normal test suite hands you a log file and wishes you luck. What AAT leaves behind instead is the run itself. The workflow uploads it with `if: always()`, because a failed run is when it is worth having:

```yaml
- name: Upload the run archives
  if: always()
  uses: actions/upload-artifact@v4
```

## Opening it

The artifact is a `.aab` — a batch, packed as one file. Download it from the run and open it:

```bash
gh run download <run-id> -R gburgyan/aat-shippo
aat import shippo-batch-20260921-065546-557e21d3.aab
aat run show <the failing run>
```

```
FAILED  10.6s
plan: A label is bought from a rate, carries a tracking number, and is refunded
error: step "refund" (createRefund) failed mechanical validation

  #  STEP      NODE               STATUS  RESULT     TIME
  1  from      createAddress         201  pass       1.2s
  2  to        createAddress         201  pass       1.2s
  3  parcel    createParcel          201  pass       1.2s
  4  shipment  createShipment        201  pass       1.8s
  5  rates     listShipmentRates     200  pass      597ms
  6  label     createTransaction     201  pass       2.9s
  7  read      getTransaction        200  pass      117ms
  8  refund    createRefund          201  FAIL       1.7s
```

Eight steps, seven of them fine. The refund returned `201 Created` and still failed, which narrows it to an assertion rather than a transport error.

## The step

```bash
aat run show <run> --step refund
```

```
step refund (node createRefund)
POST https://api.goshippo.com/refunds
status 201  FAIL  1.7s
inputs:
  async          false                               plan_default
  transactionId  "146d8511c9784a32a179dfad8b201ade"  plan_from label.transactionId
outputs:
  refundId       "061e822681504162ba4fe49decbc9130"
  status         "ERROR"
  test           true
assertions: 1 passed, 1 failed
  FAIL predicate: predicate "transactionId == \"...\" && test == true &&
       status in [\"QUEUED\", \"PENDING\", \"SUCCESS\"]" is false
```

There is the whole thing. The refund came back `ERROR`; the plan expects one of three other statuses. The `plan_from label.transactionId` line is worth noticing too — it says where that input came from, so there is no question of the wrong id having been sent.

And the response Shippo actually returned:

```json
{
  "object_id": "061e822681504162ba4fe49decbc9130",
  "status": "ERROR",
  "transaction": "146d8511c9784a32a179dfad8b201ade",
  "test": true
}
```

No message, no error field, nothing saying why.

## Comparing against the last green run

This is the step that would be impossible with a tool that keeps no history. The run from 17 September is still downloadable, so the same command answers what it used to do:

```bash
gh run download 35191197169 -R gburgyan/aat-shippo
aat run show <that run> --step refund --response
```

```json
{
  "object_id": "b15194e052ad4c0ba026b38622b1f031",
  "status": "PENDING",
  "transaction": "215ca4e1148642bd8231430c93e314fc",
  "test": true
}
```

`PENDING` on the 17th, `ERROR` on the 18th onward, from an identical request body: `{"transaction": "<id>", "async": false}`. Four consecutive nights, so not intermittent.

## Ruling out the boring explanations

Two things would have made this our bug, and both are answerable from files already on disk.

**Was the label finished when the refund was asked for?** The plan reads the transaction back immediately before refunding it, so the answer is recorded:

```bash
aat run show <run> --step read
```

`status "SUCCESS"` with a tracking number — on the failing run *and* the passing one. The label was complete both times.

**Was `ERROR` an intermediate state?** The step sends `async: false`, visible in the inputs above, so Shippo resolved the refund before replying. `ERROR` is the final answer.

Add that the repository's last commit predates the last passing run, and there is nothing left on our side to blame.

## What the vendor's own documentation says

Shippo publishes both halves of the contradiction. On [testing](https://docs.goshippo.com/docs/guides_general/testing/):

> Refund requests in test mode will always return a success response, but no invoice item will be generated.

Every response above carries `"test": true`. And on [refunds](https://docs.goshippo.com/docs/billing_and_invoices/refundinglabels/):

> **ERROR** — Refund rejected, the shipment was found to be used.

A test label created seconds earlier and never handed to a carrier cannot have been used.

Worth noting what did *not* settle this: the OpenAPI spec. `Refund.status` enumerates `QUEUED`, `PENDING`, `SUCCESS`, and `ERROR`, so `ERROR` is a legal value and [schema validation](../validation.md) has nothing to object to. `POST /refunds` is described as "Creates a new refund object." A spec describes shape; the behaviour was in the prose, and the assertion in the plan is what encoded it.

## What it cost

No reproduction environment. No credentials. No re-running anything against a live API. No asking a colleague what the response used to look like. Every fact above came out of files that existed already, because each run had recorded itself and CI had kept it.

That is the argument the rest of this site makes in the abstract — [one set of files, several jobs](../why.md#one-set-of-files-several-jobs) — with a date on it. The same plans a developer runs while working are the ones CI ran unattended at 06:55, and the thing CI left behind was not a log to scroll but a file anyone can open.

## What it did not do

AAT did not know this was a defect. It knew an assertion was false. The judgement that Shippo was wrong rather than the plan came from reading their documentation, and a person did that. Nor does any of this say *why* the behaviour changed — only that it did, precisely when, and that the request was identical either side of the change.

The assertion was deliberately not widened to accept `ERROR` while the cause was unknown. Doing so would have turned a real signal into a green tick, and asserted the opposite of what the vendor publishes. A suite that goes green by agreeing with whatever the API said last is not a suite.

## How it ended

Reported on 21 September 2026, and confirmed by Shippo as a known defect in their test environment. Live refunds behave as documented; the bug does not reach anything a real shipment touches.

That is a good outcome and a slightly deflating one, and both are worth saying. The plan was right, the documentation was right, and the API was wrong in a way that costs nobody a parcel. What it cost instead was a suite that could not tell the truth about a working integration — which is exactly what a test environment is for, and exactly the sort of thing that goes unnoticed when nothing is watching it on a schedule.

The assertion can now be settled honestly: assert what test mode currently does, behind a dated note that says why and points here, so the suite goes green on a known fact rather than on a shrug, and fails again if the behaviour changes back. That is a different thing from widening the predicate and hoping.

## Postscript

Test mode went back to answering `PENDING` on 25 September 2026, a week after it stopped. From that day the nightly reported the three covered steps as *known issue passing again*, and the entries expired on 6 October with the steps green; they came out of the plan on 1 November. Nothing in the suite was edited while the defect lasted, and the run archives hold its start, Shippo's confirmation, and its end.
