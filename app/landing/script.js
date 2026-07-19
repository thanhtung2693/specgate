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

function resetConsole() {
  consoleTimers.forEach(clearTimeout);
  consoleTimers = [];
  checks.forEach((li) => li.setAttribute("data-status", "pending"));
  probes.forEach((p) => p.setAttribute("data-on", "0"));
  if (chip) {
    chip.setAttribute("data-state", "run");
    chip.textContent = "Reviewing…";
  }
}

function runConsole() {
  resetConsole();
  if (prefersReducedMotion) {
    checks.forEach((li) => li.setAttribute("data-status", "met"));
    probes.forEach((p) => p.setAttribute("data-on", "1"));
    if (chip) {
      chip.setAttribute("data-state", "pass");
      chip.textContent = "Pass 3/3";
    }
    return;
  }
  const stepIn = (fn, delay) => consoleTimers.push(setTimeout(fn, delay));
  let t = 520;
  checks.forEach((li, i) => {
    stepIn(() => li.setAttribute("data-status", "met"), t + i * 620);
  });
  t += checks.length * 620 + 180;
  probes.forEach((p, i) => {
    stepIn(() => p.setAttribute("data-on", "1"), t + i * 260);
  });
  t += probes.length * 260 + 240;
  stepIn(() => {
    if (chip) {
      chip.setAttribute("data-state", "pass");
      chip.textContent = "Pass 3/3";
    }
  }, t);
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
      cap: { t: "Approve the contract.", d: "Publish immutable documents, run readiness, then record the human decision." },
      lines: [
        { k: "cmd", t: "$ specgate artifact publish --file artifact.json --preview" },
        { k: "out", t: "  Artifact publish preview:" },
        { k: "out", t: "  docs/spec.md  spec  4821 bytes" },
        { k: "dim", t: "  No publication performed: Human confirmation required before publishing." },
        { k: "cmd", t: "$ specgate artifact publish --file artifact.json" },
        { k: "ok", t: "  Published 8a42…" },
        { k: "cmd", t: "$ specgate gates check <artifact-id>" },
        { k: "ok", t: "  Spec quality: passed" },
        { k: "cmd", t: "$ specgate --yes artifact approve <artifact-id>" },
        { k: "pass", t: "  Approved 8a42… (v1)" },
      ],
    },
    {
      title: "specgate / approved context",
      cap: { t: "Hand off one contract.", d: "Promote the approved artifact, then the coding agent reads its Context Pack before editing." },
      lines: [
		{ k: "cmd", t: "$ specgate --yes artifact promote <artifact-id>" },
		{ k: "cmd", t: "$ specgate work create --feature local-healthcheck --title \"Add healthcheck\" --ac \"GET /healthz returns 200\"" },
        { k: "cmd", t: "$ specgate work list --phase ready" },
        { k: "out", t: "  CR-7A31D0C2  ready  Add healthcheck endpoint" },
        { k: "cmd", t: "$ specgate work context <work-ref>" },
        { k: "out", t: "  Source: approved Context Pack" },
        { k: "out", t: "  Scope: healthcheck endpoint and automated test" },
        { k: "out", t: "  Non-goal: deployment or monitoring changes" },
        { k: "pass", t: "  Implementation contract loaded" },
      ],
    },
    {
      title: "specgate / delivery review",
      cap: { t: "Verify what shipped.", d: "Re-run reported checks, record independent peer evidence, then leave the delivery decision to a human." },
      lines: [
        { k: "cmd", t: "$ specgate delivery report <work-ref> --init" },
        { k: "out", t: "  Wrote .specgate/completion-<ref>.json" },
        { k: "cmd", t: "$ specgate delivery submit <work-ref> --file .specgate/completion-<ref>.json --run-checks" },
        { k: "out", t: "  1/4  Completion report recorded" },
        { k: "out", t: "  2/4  Quality gates triggered" },
        { k: "out", t: "  3/4  Delivery review triggered" },
        { k: "out", t: "  4/4  Delivery status fetched" },
        { k: "ok", t: "  Verdict: needs_human_review" },
        { k: "cmd", t: "$ specgate delivery peer-review <work-ref> --init" },
        { k: "cmd", t: "$ specgate delivery peer-review <work-ref> --file .specgate/peer-review-<ref>.json" },
        { k: "out", t: "  Peer review recorded; delivery review rerun." },
        { k: "cmd", t: "$ specgate delivery status <work-ref> --detail" },
        { k: "pass", t: "  Verdict: pass" },
        { k: "ok", t: "  Peer review: passed" },
        { k: "dim", t: "  Human approval remains required." },
        { k: "dim", t: "  Human reviewer records the final delivery decision." },
        { k: "cmd", t: "$ specgate --yes delivery approve <work-ref>" },
      ],
    },
    {
      title: "specgate / governance signals",
      cap: { t: "Measure the loop.", d: "Read observed workflow signals from real gate and delivery history." },
      lines: [
        { k: "dim", t: "# example workspace data" },
        { k: "cmd", t: "$ specgate stats --days 30" },
        { k: "out", t: "  First-pass yield          10 / 12 reviewed" },
        { k: "out", t: "  Pre-build signals         4" },
        { k: "out", t: "  Post-build signals        3" },
        { k: "out", t: "  Rework runs               2" },
        { k: "out", t: "  Blocked-ambiguity reports 2" },
        { k: "pass", t: "  Signals are descriptive, not a quality score" },
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
          play(0);
          observer.disconnect();
        }
      });
    },
    { threshold: 0.4 },
  );
  io.observe(root);
})();
