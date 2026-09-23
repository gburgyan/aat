# Why AAT Exists

I was changing a service I could not test on its own.

It was one of a set of badly factored microservices, and reaching the one I was touching meant standing up everything in front of it first: a dozen calls to create the account, the records, and the state it expected before it would do anything interesting. What I had for that was half a dozen Postman collections, all slightly different, none of which worked all the time.

They had worked, at first. Then the team grew. Everyone had their own copy with their own tweaks, and none of them were reliable. Nothing was in source control, so there was no diff, no review, and no way to tell whose version was right. Every new test case meant editing a collection in place, so the case it replaced was gone. The chaining lived in pre-request scripts, which put the interesting part of a flow — what depends on what — inside JavaScript instead of in front of you. And none of it was legible to an AI coding tool: the export was one file too large to read, in a shape nothing else consumes.

The underlying problem is that real integrations are not one call. Buying something means browse, cart, checkout, pay, ship, and maybe return and refund: 8 to 20 calls, each needing IDs from the calls before it, leaving state behind that someone has to clean up. A collection is a folder of single requests. Everything that makes those requests a *flow* has to live somewhere else, and that somewhere was scripts.

Postman is good at what it is for: exploring an API by hand, one request at a time. It is a poor place to *keep* the knowledge of how an API works. That knowledge ends up in a format only Postman reads, in a workspace rather than your repository, and it scales by copying.

## Two things I tried first

Neither got there, and where they both stopped short turned out to be the whole idea.

**Recording a session and rebuilding it.** This one got further than it sounds. It was a proxy: point a client at it, run the flow by hand once, and it captured the traffic, then found the values that chained — a value that appeared in one response and turned up again in a later request — named them with an LLM, and wrote out a Postman collection with the extraction scripts already in it. The chaining was discovered rather than typed, which was the whole point.

It worked, and it was still a dead end, because the wiring it found was baked into the collection it produced. Those connections belonged to the recording: this response's field feeds that request's body, in this order, along the path I happened to walk. Reach the same goal another way and none of it applied, so I recorded again. That is the pile of slightly different collections I started with, except now I was generating them.

**Describing the API and letting an LLM assemble the calls.** Give a model the list of operations and a goal, and have it work out which calls reach that goal and in what order. It was a non-starter for anything but the simplest workflows, and it failed for the mirror-image reason: nothing had written the connections down, so every run was a fresh guess at them, and the guesses did not agree.

So it was never about finding the connections — the recorder found them. It was about where they live. That a checkout's order id feeds a payment is a fact about those two operations, true on every path that reaches them and in whatever order the rest of the plan takes. Attach it to the operations instead of to a run, along with what has to have happened first and what undoes what, and a path to a goal becomes something to work out rather than something to have captured. Once that was written down, the engine could run the API without being told anything else — and, as it turned out, so could a great deal besides.

The second dead end survives as [`aat prompt`](prompt.md), which drafts a plan from a sentence. It works, and it is the least load-bearing thing in the project.

So the knowledge moved into the repository. **API knowledge** is a [graph](graphs.md) of operations and [request templates](templates.md), written once. **Test intent** is a [plan](plans.md) that lists steps, not wiring. **Variation** is [layers](batch-layers.md) and [environments](environments.md) that turn one plan into a matrix. Small files, reviewed like code, that an AI coding tool can read one at a time and a person can follow without opening a debugger.

The point is not that the files are tidy. It is that they run.

## What changed, item by item

| What went wrong with a pile of collections | What AAT does instead |
|---|---|
| Everyone had a copy, with their own tweaks | One graph in the repository. Plans name steps, not wiring, so fixing an operation fixes every test that uses it |
| None of them were reliable | The project runs. [`aat validate --strict`](validation.md) catches broken wiring before a request is sent, [`--oas-validate`](running.md) checks every exchange against the spec, and the [archive](archives.md) keeps the request and the response |
| Every new case meant mutating the collection | [Layers](batch-layers.md) turn one plan into a matrix, and nothing is edited in place |
| No source control | One small YAML file per operation, reviewed in a pull request and diffed like code |
| An AI tool could not use it | A 21 MB export does not fit in a context window; a 23-line template does. The [MCP server](mcp-server.md) hands an assistant the flows as tools rather than prose |
| The scripting hid the lede | Wiring is [declared, not scripted](value-flow.md). The archive shows every value, where it came from, and every decision |

The two numbers in that last row are measured, not rhetorical. The private airline API this tool was built against ships a 40,000-line OpenAPI spec, and the team's Postman collections for it were 21 MB and 23 MB. The AAT project that replaced them is a 3,708-line graph covering 74 operations, with a median request template of 23 lines. An assistant — or a reviewer — loads the one operation the task needs, and edits one file.

## Every feature came from a problem

Each piece below is something that got annoying enough to fix, in the order it became annoying.

**Testing my own build meant editing a call by hand.** Find the endpoint in the collection, duplicate the request, point the copy at my laptop, run it, and remember to throw the copy away. [Local overrides](local-dev.md) route a single operation to a local container while everything around it runs against the real environment, with the environment's auth intact and nothing in the project edited. The code under test runs the way it will run deployed, reached by the same flow, with no new machinery.

**Debugging happened over screenshots.** A tester sends a picture of one pane; the developer asks what the request body was; the tester sends another picture. Every run now writes an [archive](archives.md) — each request and response, every resolved value, every retry and assertion — and the [web UI](web-ui.md) reads it. A run exports as a single file that whoever you send it to opens in the same viewer. Instead of describing the failure, you send it.

**Every variation was another copy of the same test.** Searching for one kind of thing exercises different code than searching for another, and I did not want a bespoke test per kind, each restating the whole flow around the one value that differed. [Layers](batch-layers.md) supply named sets of data and multiply the plans that actually read them, so a new case is a few lines rather than a new copy.

**Someone asked for an MCP server.** The graph already held every operation's exact request, what each one needs from the calls before it, and what real responses look like. If there is enough there to call the API, there is enough to describe calling it. That was [`aat mcp serve`](mcp-server.md), and it is the one on this list that turned out to be worth more than the problem it was built for.

## What else could read it

Once the API was described well enough for the engine to run it — typed operations, where each value comes from, what has to happen first, what undoes what — the description turned out to be worth more than the tests it was written for. The question stopped being *what else should this run?* and became *what else can read this?*

**AI coding tools.** [`aat mcp serve`](mcp-server.md) hands an assistant the same graph the engine runs: each operation's exact request, the order calls go in, what each one needs from the calls before it, the composed flows, and sample responses from real runs. It is a form a machine can act on, rather than prose it has to interpret. That makes the project a Rosetta stone for the API: the description is language-neutral, so an assistant reading it writes a client in whichever language you asked for. On a 74-node airline API, a demo produced a working client in a single prompt in Java, Go, Python, C#, Perl, and — because the question came up — Lisp. Package a subset as an [integration kit](integration-kit.md) and your integrators' assistants read it too. LLMs are optional and authoring-time only: `aat prompt` can draft a plan, and the MCP server teaches AI tools your API. Execution never calls an LLM.

The reason that works is the guardrails rather than the prose. [`aat validate --strict`](validation.md) rejects a misspelled field, a path that reads nothing, and wiring that cannot resolve, before a request is sent; a run either passes or says which step failed and why. An assistant can author, validate, run, and read the failure without a person in the middle, which is what makes the loop closed rather than a draft someone has to check by hand.

**Evidence you can send someone.** Every run writes an [archive](archives.md): each request and response, how every input got its value, every retry, every assertion, and the cleanup, with secrets redacted. The [web UI](web-ui.md) exports a run as a single `.aar` file, and whoever you send it to opens it in the same viewer with `aat web view` or `aat import`. It is how you show that something works — or that it doesn't — with the actual exchange instead of a screenshot of one pane, and it is the difference between "the sandbox rejects this" and a file the other team can open and step through.

![The shop's checkout step in the web UI: node, status, and display outputs, the Request tab with the method and URL, the Copy as cURL button, the headers with Authorization redacted, and the JSON body](assets/ui-step-request-curl.png)

**Reference documentation.** [`aat docs generate`](docs-generate.md) writes Markdown for every operation from the graph, so the description that runs the tests is also the page someone reads.

**Whatever comes next in your pipeline.** [`--stop-after`](checkpoints.md) stops a run at a named step and leaves the resources it created alive; `--dump-state` writes their IDs, base URLs, and headers for a pytest suite, a load test, or a `curl` session to pick up. For [CI](ci-cd.md) there are exit codes 0/1/2/130, `--json`, `--quiet`, JUnit XML, and a Docker image.

None of that was a roadmap. It is what one good description of an API turned out to be good for, and it is what the *toolkit* in the name means. *Adaptive* is the other half: the many ways one graph runs — layers, environments, per-operation overrides, checkpoints, batch matrices, the MCP server — without a copy of anything.

## One set of files, several jobs

That is worth saying in its own right, because it is the part that pays for itself twice. The plans are not a suite sitting beside the real work, kept in step with it by hand. They are one description of the API, pointed at whichever occasion is in front of you.

**The everyday test is the sign-off test.** The batch a developer runs while changing something is the batch [CI](ci-cd.md) runs on every commit, and it is the batch you run in front of the people who have to agree a release is ready. Nothing is rewritten for the occasion, and nobody maintains a separate acceptance pack that quietly stops matching what the tests actually do.

**Local debugging is that same run, aimed differently.** [Local overrides](local-dev.md) point one operation at a container on your laptop and leave the rest of the flow running against the real environment, with its auth intact. You do not fork the suite to exercise your build; you take the run you already had and move one node.

**What a pipeline leaves behind is an artifact, not a log.** Each run and batch packs into a single file the [web UI](web-ui.md) opens — every request and response, every resolved value, every retry and assertion. A failure at three in the morning is something the next person opens and steps through, rather than scrollback they have to reconstruct. The four projects below do this on every run, and the files are public: take the artifact from a recent run of one of their workflows — GitHub asks you to be signed in, and nothing more — and open it with `aat web view`. It is the same file their maintainer looks at. [What a nightly run caught](examples/nightly-catch.md) is one of those files doing its job: a scheduled run found a provider changing its test API overnight, and working out exactly what had changed took one download.

And because there is one description rather than a copy per person, improvement compounds. A template corrected, a case added, an assertion tightened: each lands once, and every plan that touches that operation has it from then on, for whoever runs it next — including the integrators you hand an [integration kit](integration-kit.md) to. A pile of collections cannot do that. Each copy improves alone, and the good fix stays with whoever made it.

## It is legible both ways

The same property that makes the files fit an agent's context window makes them fit a reviewer's head: one operation per file, one plan per scenario, no hidden scripting between them. What is good for an assistant here is good for a person for the same reason — the API is broken into pieces small enough to hold one at a time, so you do not have to understand the whole thing to find the one thing you are looking for. None of it requires an LLM to be useful. When you want detail rather than summary, the [web UI](web-ui.md) turns a run into a timeline of every step, with the resolved value behind every input, every retry and assertion, and Copy-as-cURL on any step.

## The proof is that it runs

Four complete projects against real, public APIs, each built openly: three run against an API's live test mode, and the fourth against the database itself, in a local container. Every claim in their READMEs is something a run recorded — and each project runs itself in CI and keeps the recording, so the claims are not only reproducible, they are downloadable.

| Project | Scale | What it shows |
|---|---|---|
| [aat-duffel](https://github.com/gburgyan/aat-duffel) | 66 operations, 47 plans, 14 layers; the full batch passes 47/47 in about 3½ minutes | An API with **no official OpenAPI spec**. Everything the README says about Duffel came from runs |
| [aat-stripe](https://github.com/gburgyan/aat-stripe) | 82 operations, 53 plans, 14 layers; 53/53 in about 5 minutes | About 6,300 lines of graph and templates against Stripe's **205,000-line** vendored spec, with every request and response checked against it as it goes |
| [aat-shippo](https://github.com/gburgyan/aat-shippo) | 46 of 70 operations, 28 plans, 9 layers; 28/28 in about 2½ minutes | Layers as the headline — a lane × parcel matrix and six deterministic tracking fixtures — with real shipping labels rendered in the web UI |
| [aat-qdrant](https://github.com/gburgyan/aat-qdrant) | all 52 public unary gRPC methods as 77 operations, 38 plans, 6 layers; 38/38 in about 70 seconds | The same files over **[gRPC](grpc.md)**: about 5,400 lines of YAML against a 4,700-line proto surface nobody on the AAT side wrote, every node checked offline against Qdrant's published protos, and every refusal asserted by status name and exact message |

`aat-stripe` is also the clearest example of what the description is for. It was built by signing up for a Stripe account in test mode, putting the key in an environment variable, pointing an AI coding assistant at Stripe's documentation and AAT's, and asking for tests — then about an hour of the assistant working through the API, running what it wrote, and fixing what came back. The graph is the part that made that an iteration loop rather than a guess: every attempt either validated and ran, or said exactly which field was wrong.

That loop does not need a person steering it. Before any of the four projects were built, coding agents in clean rooms, given only AAT's published docs and the API's public documentation, built a Duffel project twice and a Stripe project three times, from one prompt each; the Stripe builds took about half an hour apiece. Every attempt validated clean and passed every plan it wrote: the Duffel runs handled 13 of the 14 flows asked for unaided, the Stripe runs 13 of 13. The projects in the table are separate builds with more human curation, and they have done real work since — one of them [caught a regression](examples/nightly-catch.md) in Shippo's test environment that Shippo confirmed.

`aat-shippo` makes the argument on this page in one command. Its lane × parcel matrix runs eight combinations from two plan files. A second matrix, over an axis those plans never read, expands to fourteen runs — seven execute, seven are skipped as duplicates, ten seconds, nothing bought. A layer only multiplies the plans it actually reaches. With collections, every one of those combinations is a copy you maintain by hand.

`aat-qdrant` makes a different one: the argument does not depend on the protocol. The graph, the plans, the layers, and the archives are the same files over gRPC as over REST. It was built to find what AAT's gRPC support had not thought of, and twelve changes to AAT came out of building it, each made as the gap turned up.

[Real APIs](examples/real-apis.md) says what each covers, proves, and leaves out. Three smaller projects ship in this repository and need no account at all: the [shop](examples/shop.md) and [gRPC payments](grpc.md#the-60-second-version), which run offline against `aat-sandbox`, and the [petstore](petstore-walkthrough.md). See [Examples](examples/index.md).

## Other tools script the flow too

Postman is not the only tool that keeps the wiring in the test. Bruno puts collections in plain files in your repository and chains calls with JavaScript. Karate chains them in its own scenario language. Hurl chains them with captures written into each file. All three fix real problems with Postman — Bruno and Hurl files diff and review like code — but the connection between two calls is still written inside a particular test. A second path to the same goal writes it again, and when an operation changes, every flow that spelled out its wiring has to change with it. That is the fragility of the Postman pile, now under source control. Spec-driven fuzzers such as Schemathesis are a different thing and a good complement: they go looking for inputs that break a single operation, and do not know your flows. AAT's own [`--fuzz`](fuzzing.md) works from the other side: the plan builds the state, and the fuzzer attacks one step inside the flow.

Scripted wiring is also hard on an agent, because nothing checks it until the requests run. A mistake shows up at runtime — at best as a clear error, often as a server rejecting a value a step later, in words that say nothing about where the value came from — and every check costs a pass against the live API. The agent works backwards from a symptom. In AAT the wiring is declared on the operations, so it can be checked before anything is sent. An unknown key is an error with the line and the likely intended name; [`aat validate --strict`](validation.md) resolves every input from where the graph says it comes from; and a run that fails names the step and the assertion. That is [the loop above](#what-else-could-read-it), and it is what AAT brings to the interface between agents and APIs: guardrails. An agent working inside them edits one small file and tries again, instead of guessing. A person working inside them gets the same thing.

## If you already have API tooling

- **An OpenAPI spec** is the best starting point. [`aat generate --oas`](generate.md) scaffolds the graph and one template per operation — roughly the mechanical 70% — and you add the part a spec cannot describe: which calls reach a goal, in what order, and what undoes what.
- **A Postman collection** has no importer, and this page will not pretend otherwise. What works today is to point your AI coding assistant at the collection through a reader such as [expost](https://github.com/gburgyan/expost) and have it author the graph, then close the loop with `aat validate --strict` and a real run. The [AI assistant primer](llms.md) covers that workflow.
- **Nothing yet** is fine too. The [Tutorial](tutorial.md) builds a project by hand against the offline sandbox in about 45 minutes.
