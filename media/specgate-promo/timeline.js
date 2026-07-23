window.__timelines = window.__timelines || {};

var tl = gsap.timeline({ paused: true });

function swapHeadline(from, to, at) {
  tl.to(from, { y: -84, opacity: 0, duration: 0.28, ease: "power3.in" }, at);
  tl.fromTo(to, { y: 84, opacity: 0 }, { y: 0, opacity: 1, duration: 0.42, ease: "power4.out" }, at + 0.16);
}

function swapCommand(from, to, at) {
  tl.to(from, { y: -17, opacity: 0, duration: 0.18, ease: "power2.in" }, at);
  tl.fromTo(to, { y: 17, opacity: 0 }, { y: 0, opacity: 1, duration: 0.24, ease: "power3.out" }, at + 0.12);
}

// Beat 1: the claim enters quickly. The proof lane stays quiet.
tl.fromTo(".product-bar", { y: -24, opacity: 0 }, { y: 0, opacity: 1, duration: 0.34, ease: "expo.out" }, 0.08);
tl.fromTo(".eyebrow", { x: -34, opacity: 0 }, { x: 0, opacity: 1, duration: 0.28, ease: "power3.out" }, 0.16);
tl.fromTo("#headline-claim", { y: 72, opacity: 0 }, { y: 0, opacity: 1, duration: 0.48, ease: "power4.out" }, 0.22);
tl.fromTo("#headline-note", { x: -28, opacity: 0 }, { x: 0, opacity: 1, duration: 0.34, ease: "power3.out" }, 0.52);
tl.fromTo(".proof-surface", { y: 64, scale: 0.975, opacity: 0 }, { y: 0, scale: 1, opacity: 1, duration: 0.55, ease: "expo.out" }, 0.28);
tl.fromTo(".surface-top > *", { y: 14, opacity: 0 }, { y: 0, opacity: 1, duration: 0.25, stagger: 0.08, ease: "power2.out" }, 0.66);
tl.fromTo(".proof-node", { y: 18, opacity: 0 }, { y: 0, opacity: 1, duration: 0.28, stagger: 0.07, ease: "power3.out" }, 0.72);
tl.fromTo(".terminal-strip", { y: 74, opacity: 0 }, { y: 0, opacity: 1, duration: 0.34, ease: "power3.out" }, 0.86);
tl.set("#lane-progress", { scaleX: 0 }, 0);

// Beat 2: the evidence runner clears AC-01 and breaks at AC-02.
swapHeadline("#headline-claim", "#headline-gap", 1.42);
tl.to("#headline-note", { opacity: 0, duration: 0.16 }, 1.48);
tl.set("#headline-note", { textContent: "SpecGate checks each acceptance criterion against delivery evidence." }, 1.7);
tl.fromTo("#headline-note", { y: 12, opacity: 0 }, { y: 0, opacity: 1, duration: 0.28, ease: "power2.out" }, 1.7);
tl.to("#phase-number", { textContent: "02", duration: 0.01 }, 1.62);
tl.to("#surface-state-copy", { opacity: 0, duration: 0.12 }, 1.66);
tl.set("#surface-state-copy", { textContent: "Checking evidence" }, 1.79);
tl.to("#surface-state-copy", { opacity: 1, duration: 0.16 }, 1.8);
tl.to(".lane-runner", { x: 370, duration: 0.8, ease: "power2.inOut" }, 1.82);
tl.to("#lane-progress", { scaleX: 0.333, duration: 0.8, ease: "power2.inOut" }, 1.82);
tl.to(".node-ac1 .node-dot", {
  backgroundColor: "#4bd178",
  boxShadow: "0 0 0 2px #4bd178, 0 0 18px rgba(75, 209, 120, 0.55)",
  duration: 0.2,
}, 2.58);
tl.to(".node-ac1", { color: "#4bd178", duration: 0.2 }, 2.58);
tl.to(".node-ac1 .node-result", { textContent: "✓ reproduced", color: "#4bd178", duration: 0.01 }, 2.58);
tl.to(".lane-runner", { x: 746, duration: 0.7, ease: "power2.inOut" }, 2.76);
tl.to("#lane-progress", { scaleX: 0.668, duration: 0.7, ease: "power2.inOut" }, 2.76);
tl.to(".lane-runner", {
  backgroundColor: "#f0ae46",
  boxShadow: "0 0 0 7px rgba(240, 174, 70, 0.14), 0 0 24px rgba(240, 174, 70, 0.9)",
  duration: 0.16,
}, 3.4);
tl.to(".node-ac2 .node-dot", {
  backgroundColor: "#f0ae46",
  boxShadow: "0 0 0 2px #f0ae46, 0 0 20px rgba(240, 174, 70, 0.62)",
  duration: 0.16,
}, 3.4);
tl.to(".node-ac2", { color: "#f0ae46", duration: 0.16 }, 3.4);
tl.to(".node-ac2 .node-result", { textContent: "✕ evidence failed", color: "#f0ae46", duration: 0.01 }, 3.4);
tl.fromTo(".evidence-gap", { x: 54, opacity: 0 }, { x: 0, opacity: 1, duration: 0.36, ease: "expo.out" }, 3.48);
tl.to("#surface-state-copy", { textContent: "Evidence gap found", color: "#f0ae46", duration: 0.01 }, 3.56);
tl.to(".state-dot", {
  backgroundColor: "#f0ae46",
  boxShadow: "0 0 18px rgba(240, 174, 70, 0.7)",
  duration: 0.18,
}, 3.56);
tl.to("#result-copy", { textContent: "expected 503 · received 500", color: "#f0ae46", duration: 0.01 }, 3.62);

// Beat 3: a requested peer review turns the gap into one focused correction.
tl.to(".evidence-gap", { x: -90, opacity: 0, duration: 0.28, ease: "power2.in" }, 4.36);
swapCommand("#command-submit", "#command-peer", 4.38);
tl.to("#phase-number", { textContent: "03", duration: 0.01 }, 4.46);
tl.to("#headline-note", { opacity: 0, duration: 0.16 }, 4.46);
tl.set("#headline-note", { textContent: "A requested peer review explains exactly what changed." }, 4.64);
tl.to("#headline-note", { opacity: 1, duration: 0.24 }, 4.66);
tl.to("#peer-connector", { opacity: 1, duration: 0.22 }, 4.56);
tl.fromTo(".peer-note", { x: 62, opacity: 0 }, { x: 0, opacity: 1, duration: 0.44, ease: "expo.out" }, 4.62);
tl.to("#surface-state-copy", { textContent: "Peer review requested", color: "#f5f6f7", duration: 0.01 }, 4.64);
tl.to("#result-copy", { textContent: "review context prepared", color: "#b9bec8", duration: 0.01 }, 4.64);
swapHeadline("#headline-gap", "#headline-review", 5.0);
tl.fromTo(".peer-note code", { x: 26, opacity: 0 }, { x: 0, opacity: 1, duration: 0.3, ease: "power3.out" }, 5.42);

// The human, not the reviewer, decides to send the work back.
tl.fromTo(".human-decision", { x: -56, opacity: 0 }, { x: 0, opacity: 1, duration: 0.36, ease: "expo.out" }, 7.28);
swapCommand("#command-peer", "#command-changes", 7.34);
tl.to("#result-copy", { textContent: "focused changes requested", color: "#8993ff", duration: 0.01 }, 7.55);
tl.to(".human-decision", { backgroundColor: "#171b3c", duration: 0.28, yoyo: true, repeat: 1, ease: "power2.inOut" }, 7.64);

// Beat 4: revision repairs the same trace and returns control to the human.
tl.to(".human-decision", { x: -50, opacity: 0, duration: 0.25, ease: "power2.in" }, 9.0);
tl.to(".peer-note", { x: 54, opacity: 0, duration: 0.25, ease: "power2.in" }, 9.0);
tl.to("#peer-connector", { opacity: 0, duration: 0.18 }, 9.02);
tl.to(".lane-runner", {
  backgroundColor: "#4bd178",
  boxShadow: "0 0 0 7px rgba(75, 209, 120, 0.14), 0 0 24px rgba(75, 209, 120, 0.85)",
  duration: 0.18,
}, 9.18);
tl.to(".node-ac2 .node-dot", {
  backgroundColor: "#4bd178",
  boxShadow: "0 0 0 2px #4bd178, 0 0 18px rgba(75, 209, 120, 0.55)",
  duration: 0.22,
}, 9.18);
tl.to(".node-ac2", { color: "#4bd178", duration: 0.2 }, 9.18);
tl.to(".node-ac2 .node-result", { textContent: "✓ reproduced", color: "#4bd178", duration: 0.01 }, 9.18);
tl.fromTo(".revision-proof", { y: 28, opacity: 0 }, { y: 0, opacity: 1, duration: 0.4, ease: "power4.out" }, 9.28);
tl.to("#surface-state-copy", { textContent: "Ready for human review", color: "#4bd178", duration: 0.01 }, 9.34);
tl.to(".state-dot", {
  backgroundColor: "#4bd178",
  boxShadow: "0 0 18px rgba(75, 209, 120, 0.65)",
  duration: 0.18,
}, 9.34);
tl.to("#result-copy", { textContent: "AC-02 reproduced · peer affirmed", color: "#4bd178", duration: 0.01 }, 9.34);
tl.to(".lane-runner", { x: 1122, duration: 0.76, ease: "power2.inOut" }, 9.64);
tl.to("#lane-progress", { scaleX: 1, stroke: "#4bd178", duration: 0.76, ease: "power2.inOut" }, 9.64);
tl.to(".node-human .node-dot", {
  backgroundColor: "#8993ff",
  boxShadow: "0 0 0 2px #8993ff, 0 0 18px rgba(137, 147, 255, 0.55)",
  duration: 0.2,
}, 10.36);
tl.to(".node-human", { color: "#8993ff", duration: 0.2 }, 10.36);
tl.to(".node-human .node-result", { textContent: "ready to decide", color: "#8993ff", duration: 0.01 }, 10.36);

// The resolved outcome is concise: ready, accept, receipt, end.
swapHeadline("#headline-review", "#headline-ready", 11.76);
tl.to("#phase-number", { textContent: "04", duration: 0.01 }, 11.92);
tl.to("#headline-note", { opacity: 0, duration: 0.16 }, 11.86);
tl.set("#headline-note", { textContent: "The human accepts only after the evidence matches the work." }, 12.04);
tl.to("#headline-note", { opacity: 1, duration: 0.24 }, 12.06);
swapCommand("#command-changes", "#command-accept", 12.08);
tl.to("#result-copy", { textContent: "awaiting human acceptance", color: "#8993ff", duration: 0.01 }, 12.3);
tl.to("#command-accept", { scale: 0.96, color: "#4bd178", duration: 0.14, ease: "power1.in" }, 13.45);
tl.to("#command-accept", { scale: 1, duration: 0.28, ease: "back.out(1.5)" }, 13.59);
tl.to(".node-human .node-dot", {
  backgroundColor: "#4bd178",
  boxShadow: "0 0 0 2px #4bd178, 0 0 22px rgba(75, 209, 120, 0.65)",
  duration: 0.2,
}, 13.61);
tl.to(".node-human", { color: "#4bd178", duration: 0.2 }, 13.61);
tl.to(".node-human .node-result", { textContent: "✓ accepted", color: "#4bd178", duration: 0.01 }, 13.61);
tl.to("#surface-state-copy", { textContent: "Human accepted", color: "#4bd178", duration: 0.01 }, 13.64);
tl.to("#result-copy", { textContent: "human accepted · evidence retained", color: "#4bd178", duration: 0.01 }, 13.64);
swapHeadline("#headline-ready", "#headline-delivered", 13.5);
tl.to([".proof-lane", ".finding-stage"], { opacity: 0, duration: 0.22, ease: "power2.in" }, 13.72);
tl.fromTo(".delivered-state", { scale: 0.98, opacity: 0 }, { scale: 1, opacity: 1, duration: 0.42, ease: "power3.out" }, 13.86);
tl.fromTo(".delivered-mark", { scale: 0.55, rotation: -14 }, { scale: 1, rotation: 0, duration: 0.36, ease: "expo.out" }, 14.03);
tl.fromTo(".delivered-state > div:last-child", { x: 24, opacity: 0 }, { x: 0, opacity: 1, duration: 0.32, ease: "power3.out" }, 14.08);
tl.fromTo(".delivered-orbit", { scale: 0.7, opacity: 0 }, { scale: 1, opacity: 1, duration: 0.48, ease: "power2.out" }, 14.04);

window.__specgatePromoTimeline = tl;
