/* SpecGate landing - interactions: theme toggle, scroll reveals, verdict console. */

/* ---------- Theme toggle ---------- */
const themeToggle = document.querySelector(".theme-toggle");

function paintToggle(theme) {
  if (!themeToggle) return;
  const isLight = theme === "light";
  const nextLabel = isLight ? "Dark" : "Light";
  themeToggle.setAttribute("aria-pressed", String(isLight));
  themeToggle.setAttribute("aria-label", `${nextLabel} theme`);
  const label = themeToggle.querySelector(".theme-label");
  if (label) label.textContent = nextLabel;
}

paintToggle(document.documentElement.dataset.theme || "dark");

themeToggle?.addEventListener("click", () => {
  const next = (document.documentElement.dataset.theme || "dark") === "light" ? "dark" : "light";
  document.documentElement.dataset.theme = next;
  localStorage.setItem("specgate-landing-theme", next);
  paintToggle(next);
});

/* ---------- Scroll reveals ---------- */
const revealObserver = new IntersectionObserver(
  (entries, observer) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add("in");
        observer.unobserve(entry.target);
      }
    });
  },
  { rootMargin: "0px 0px -4% 0px", threshold: 0.05 },
);

document.querySelectorAll(".reveal-up").forEach((el) => revealObserver.observe(el));

/* ---------- Verdict console ---------- */
const chip = document.querySelector("#verdict-chip");
const checks = Array.from(document.querySelectorAll("#check-list li"));
const probes = Array.from(document.querySelectorAll("#console-checks .chk"));
const replayBtn = document.querySelector("#console-replay");

let consoleTimers = [];
const prefersReducedMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;

function showConsoleState(state, label) {
  chip.setAttribute("data-state", state);
  chip.textContent = label;
}

function runConsole() {
  consoleTimers.forEach(clearTimeout);
  consoleTimers = [];
  if (!chip || prefersReducedMotion) return;

  showConsoleState("run", "Agent says done");
  checks.forEach((li) => li.setAttribute("data-status", "pending"));
  probes.forEach((probe) => probe.setAttribute("data-on", "0"));
  const stepIn = (fn, delay) => consoleTimers.push(setTimeout(fn, delay));
  stepIn(() => {
    showConsoleState("run", "Reading report");
    probes.forEach((probe) => probe.setAttribute("data-on", "1"));
  }, 500);
  checks.forEach((li, i) => {
    stepIn(() => li.setAttribute("data-status", li.dataset.result), 800 + i * 430);
  });
  stepIn(() => showConsoleState("gap", "Evidence gap"), 1330);
}

replayBtn?.addEventListener("click", runConsole);

if (chip && checks.length) {
  // Kick off once the console scrolls into view.
  const consoleEl = document.querySelector(".console");
  if (consoleEl) {
    const once = new IntersectionObserver(
      (entries, observer) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            runConsole();
            observer.disconnect();
          }
        });
      },
      { threshold: 0.4 },
    );
    once.observe(consoleEl);
  }
}

/* ---------- Scrollspy nav ---------- */
const navLinks = Array.from(document.querySelectorAll(".nav-links a"));
const spyTargets = navLinks
  .map((a) => {
    const id = a.getAttribute("href")?.slice(1);
    const section = id && document.getElementById(id);
    return section ? { a, section } : null;
  })
  .filter(Boolean);

if (spyTargets.length) {
  const spy = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        const match = spyTargets.find((t) => t.section === entry.target);
        if (!match) return;
        navLinks.forEach((a) => a.classList.remove("active"));
        match.a.classList.add("active");
      });
    },
    { rootMargin: "-45% 0px -50% 0px" },
  );
  spyTargets.forEach((t) => spy.observe(t.section));
}

/* ---------- See-it-run: current CLI value path ---------- */
(function initCastCarousel() {
  const root = document.querySelector("[data-carousel]");
  if (!root) return;

  const CASTS = [
    {
      title: "specgate / approved artifact",
      cap: { t: "Approve one snapshot.", d: "Publish to your local store, run readiness checks, then record the human decision." },
      lines: [
        { k: "cmd", t: "$ specgate artifact publish --file artifact.json --preview" },
        { k: "out", t: "  Artifact publish preview:" },
        { k: "out", t: "  docs/spec.md  spec  4821 bytes" },
        { k: "dim", t: "  Preview only. Confirm before publishing to the Local store." },
        { k: "cmd", t: "$ specgate artifact publish --file artifact.json" },
        { k: "ok", t: "  Local snapshot created" },
        { k: "cmd", t: "$ specgate gates check <artifact-id>" },
        { k: "dim", t: "  If IDE readiness tasks are pending, complete them before approval." },
        { k: "cmd", t: "$ specgate gates results <artifact-id>" },
        { k: "ok", t: "  Spec quality: passed" },
        { k: "cmd", t: "$ specgate --yes artifact approve <artifact-id>" },
        { k: "pass", t: "  Human approval recorded (v1)" },
      ],
    },
    {
      title: "specgate / approved context",
      cap: { t: "Resume the right work.", d: "A resumed session uses the same approved criteria and pinned documents." },
      lines: [
        { k: "cmd", t: "$ specgate --yes artifact promote <artifact-id>" },
        { k: "cmd", t: "$ specgate work create --feature local-healthcheck --title \"Add health check\" --ac \"Unhealthy returns 503 @check:unit\"" },
        { k: "cmd", t: "$ specgate work list --phase ready" },
        { k: "out", t: "  Ready  Add health check" },
        { k: "cmd", t: "$ specgate work resume <work-ref> --json" },
        { k: "cmd", t: "$ specgate work context <work-ref> --document docs/spec.md --role spec --json" },
        { k: "out", t: "  Source: approved Context Pack" },
        { k: "out", t: "  Scope: health status function and unit tests" },
        { k: "out", t: "  Non-goal: deployment or monitoring changes" },
        { k: "pass", t: "  Implementation contract loaded" },
        { k: "cmd", t: "$ specgate --yes work checkpoint <work-ref> --note handoff --json" },
        { k: "dim", t: "  Keep the returned checkpoint ID for an explicit later comparison." },
      ],
    },
    {
      title: "specgate / delivery review",
      cap: { t: "See which test ran.", d: "The suite exits zero while omitting the agreed unit test. After the agent adds it, inspect the new result before deciding." },
      lines: [
        { k: "dim", t: "# report-enabled Local pin configured; follow the linked walkthrough" },
        { k: "cmd", t: "$ specgate delivery report <work-ref> --init --json" },
        { k: "dim", t: "  Use the returned data.path as <completion-path>; fill in the evidence." },
        { k: "cmd", t: "$ specgate change submit <work-ref> --file <completion-path> --run-checks --yes" },
        { k: "out", t: "  State: implementation" },
        { k: "out", t: "  Evidence: Evidence gaps found" },
        { k: "warn", t: "  [unmet] Unhealthy returns 503" },
        { k: "warn", t: "  selected testcase is missing" },
        { k: "dim", t: "# Agent adds the unit test, updates the report, and keeps the reviewed pin." },
        { k: "cmd", t: "$ specgate change submit <work-ref> --file <completion-path> --run-checks --yes" },
        { k: "out", t: "  State: awaiting_acceptance" },
        { k: "cmd", t: "$ specgate change status <work-ref> --checkpoint <checkpoint-id> --detail" },
        { k: "pass", t: "  selected test: test/unhealthy returns 503 passed" },
        { k: "warn", t: "  stale risk: watched input health.test.mjs changed" },
        { k: "out", t: "  Next actor: human_reviewer" },
        { k: "dim", t: "  Human approval remains required. Inspect the changed test before deciding." },
        { k: "dim", t: "# Human action after review. Use the exact command returned by status." },
        { k: "cmd", t: "$ specgate --yes change accept <work-ref> --review-id <review-id> --basis-digest <basis-digest> --checkpoint <checkpoint-id>" },
      ],
    },
    {
      title: "specgate / review handoff",
      cap: { t: "Share a review request.", d: "For eligible reviews, a teammate can read the request beside the diff. The originating Local store records the decision." },
      lines: [
        { k: "dim", t: "# From the checkout with a reviewed delivery eligible for export:" },
        { k: "cmd", t: "$ specgate delivery handoff export <work-ref> --file review.json" },
        { k: "dim", t: "# Inspect the unsigned request before committing it alongside the change." },
        { k: "cmd", t: "$ specgate delivery handoff show --file review.json" },
        { k: "out", t: "  Read the work, completion evidence and recorded delivery review." },
        { k: "dim", t: "  Record the human decision in the originating Local store." },
      ],
    },
  ];

  const segs = Array.from(root.querySelectorAll("[data-cast-tab]"));
  if (segs.length !== CASTS.length) return;

  const titleEl = root.querySelector("[data-cast-title]");
  const body = root.querySelector("[data-cast-body]");
  const capEl = root.querySelector("[data-cast-caption]");
  const N = CASTS.length;
  if (!titleEl || !body || !capEl) return;

  let timers = [];
  let runId = 0;

  const clear = () => {
    timers.forEach(clearTimeout);
    timers = [];
  };

  function buildLines(cast) {
    body.replaceChildren();
    return cast.lines.map((ln) => {
      const span = document.createElement("span");
      span.className = "tl show";
      span.dataset.k = ln.k;
      body.appendChild(span);
      body.appendChild(document.createTextNode("\n"));
      return { ...ln, span };
    });
  }

  function setActive(idx) {
    const cap = CASTS[idx].cap;
    titleEl.textContent = CASTS[idx].title;
    body.setAttribute("aria-labelledby", segs[idx].id);
    capEl.style.opacity = "0";
    timers.push(
      setTimeout(() => {
        const title = document.createElement("b");
        title.textContent = cap.t;
        const detail = document.createElement("span");
        detail.textContent = cap.d;
        capEl.replaceChildren(title, document.createTextNode(" "), detail);
        capEl.style.opacity = "1";
      }, 180),
    );
    segs.forEach((b, i) => {
      b.setAttribute("aria-selected", String(i === idx));
      b.setAttribute("tabindex", i === idx ? "0" : "-1");
    });
  }

  function play(idx) {
    clear();
    const token = ++runId;
    setActive(idx);
    const cast = CASTS[idx];
    const lines = buildLines(cast);

    if (prefersReducedMotion) {
      lines.forEach((l) => (l.span.textContent = l.t));
      return;
    }

    let li = 0;
    const next = () => {
      if (token !== runId) return;
      if (li >= lines.length) {
        timers.push(
          setTimeout(() => {
            if (token === runId) play((idx + 1) % N);
          }, 2400),
        );
        return;
      }
      const l = lines[li++];
      if (l.k === "cmd") {
        l.span.classList.add("cur");
        let c = 0;
        const type = () => {
          if (token !== runId) return;
          l.span.textContent = l.t.slice(0, c);
          if (c < l.t.length) {
            c++;
            timers.push(setTimeout(type, 12));
          } else {
            l.span.classList.remove("cur");
            timers.push(setTimeout(next, 220));
          }
        };
        type();
      } else {
        l.span.textContent = l.t;
        timers.push(setTimeout(next, 240));
      }
    };
    timers.push(setTimeout(next, 220));
  }

  segs.forEach((button, i) => {
    button.addEventListener("click", () => play(i));
    button.addEventListener("keydown", (event) => {
      const keys = { ArrowRight: 1, ArrowLeft: -1 };
      let next = keys[event.key] === undefined ? i : (i + keys[event.key] + N) % N;
      if (event.key === "Home") next = 0;
      if (event.key === "End") next = N - 1;
      if (next === i && !["Home", "End"].includes(event.key)) return;
      event.preventDefault();
      segs[next].focus();
      play(next);
    });
  });

  if (prefersReducedMotion) {
    play(0);
    return;
  }
  const io = new IntersectionObserver(
    (entries, observer) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          if (runId === 0) play(0);
          observer.disconnect();
        }
      });
    },
    { threshold: 0.4 },
  );
  io.observe(root);
})();
