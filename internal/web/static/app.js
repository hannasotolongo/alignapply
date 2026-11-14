"use strict";

/*
 * Jane Careers web app.
 *
 * No framework and no build step: the Go server embeds and serves these files.
 * All DOM is built with the h() helper, which only ever sets text content, so
 * job descriptions, résumé text, and user input can never inject markup.
 */

const BRAND = "Jane Careers";

// ---------------------------------------------------------------------------
// DOM helpers
// ---------------------------------------------------------------------------

function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (value === undefined || value === null || value === false) continue;
    if (key === "class") el.className = value;
    else if (key.startsWith("on") && typeof value === "function") el.addEventListener(key.slice(2), value);
    else if (key === "value") el.value = value;
    else if (value === true) el.setAttribute(key, "");
    else el.setAttribute(key, String(value));
  }
  appendChildren(el, children);
  return el;
}

function appendChildren(el, children) {
  for (const child of children.flat(Infinity)) {
    if (child === null || child === undefined || child === false) continue;
    el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

function mount(node) {
  document.getElementById("app").replaceChildren(node);
  window.scrollTo(0, 0);
}

function link(href, attrs, ...children) {
  return h("a", { href, ...attrs, onclick: (event) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    navigate(href);
  } }, ...children);
}

let toastTimer;
function toast(message, isError) {
  const el = document.getElementById("toast");
  el.textContent = message;
  el.className = isError ? "toast error" : "toast";
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 4200);
}

function spinnerRow(text) {
  return h("div", { class: "row muted" }, h("div", { class: "spinner" }), text);
}

function formatDate(value) {
  return new Date(value).toLocaleDateString(undefined, { dateStyle: "medium" });
}

// ---------------------------------------------------------------------------
// API
// ---------------------------------------------------------------------------

class ApiError extends Error {
  constructor(status, message, code) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function api(path, options = {}) {
  const init = { method: options.method || "GET", headers: {}, credentials: "same-origin" };
  if (options.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }

  const response = await fetch(path, init);
  const text = await response.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }

  if (!response.ok) {
    if (response.status === 401 && !options.allowUnauthorized) {
      state.user = null;
      navigate("/login", { replace: true });
    }
    const message = (data && data.error) || text.trim() || `Request failed (${response.status})`;
    throw new ApiError(response.status, message, data && data.code);
  }

  return data;
}

// downloadDocument posts JSON to a document endpoint and saves the file the
// server sends back (PDF or Word), using the server's file name.
async function downloadDocument(path, format, body) {
  const response = await fetch(`${path}?format=${encodeURIComponent(format)}`, {
    method: "POST", credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    const text = await response.text();
    let data = null;
    try { data = JSON.parse(text); } catch { data = null; }
    if (response.status === 401) { state.user = null; navigate("/login", { replace: true }); }
    throw new ApiError(response.status, (data && data.error) || text.trim() || `Download failed (${response.status})`, data && data.code);
  }
  const blob = await response.blob();
  const match = /filename="([^"]+)"/.exec(response.headers.get("Content-Disposition") || "");
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = match ? match[1] : `document.${format}`;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30000);
}

// downloadButtons renders "PDF" and "Word" buttons for one document.
function downloadButtons(path, getBody, onError) {
  const make = (format, label) => {
    const button = h("button", { class: "btn btn-sm", type: "button", onclick: async () => {
      button.disabled = true;
      const old = button.textContent;
      button.textContent = "Preparing…";
      try { await downloadDocument(path, format, await getBody()); }
      catch (error) { if (onError) onError(error); }
      finally { button.disabled = false; button.textContent = old; }
    } }, label);
    return button;
  };
  return [make("pdf", "↓ PDF"), make("docx", "↓ Word")];
}

// fetchMatchPage asks for one page of ranked, scored matches. The server
// says how many there are in all and where the next page starts.
const MATCH_PAGE_SIZE = 20;
async function fetchMatchPage(request, offset) {
  const response = await fetch("/api/v1/job-matches", {
    method: "POST", credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...request, offset, pageSize: MATCH_PAGE_SIZE }),
  });
  const text = await response.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }
  if (!response.ok) {
    if (response.status === 401) { state.user = null; navigate("/login", { replace: true }); }
    throw new ApiError(response.status, (data && data.error) || text.trim() || `Request failed (${response.status})`, data && data.code);
  }
  const matches = Array.isArray(data) ? data : [];
  const total = Number(response.headers.get("X-Jane-Total")) || matches.length;
  const nextOffset = Number(response.headers.get("X-Jane-Next-Offset")) || offset + matches.length;
  // Free plan: how many more scored jobs Pro would show.
  const locked = Number(response.headers.get("X-Jane-Locked")) || 0;
  return { matches, total, nextOffset, locked };
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

const state = {
  user: undefined,       // undefined = not loaded, null = signed out
  billing: null,
  plans: null,
  profile: null,
  preferences: null,
  resumes: null,
  resumeText: new Map(), // résumé id → text, fetched on demand
  lastSearch: null,      // { request, matches, resumeId }
};

function resetState() {
  Object.assign(state, {
    user: null, billing: null, profile: null, preferences: null,
    resumes: null, lastSearch: null,
  });
  state.resumeText.clear();
}

async function loadUser() {
  try {
    state.user = await api("/api/v1/me", { allowUnauthorized: true });
  } catch {
    state.user = null;
  }
  return state.user;
}

async function loadBilling() {
  try {
    state.billing = await api("/api/v1/billing/status");
  } catch (error) {
    if (error.status !== 401) console.error(error);
  }
  return state.billing;
}

async function loadPlans() {
  if (state.plans) return state.plans;
  try {
    state.plans = await api("/api/v1/billing/plans");
  } catch {
    state.plans = { billingEnabled: false, plans: [], freeJobMatchesPerWeek: 3, gmailEnabled: false };
  }
  return state.plans;
}

async function loadProfile(force) {
  if (!state.profile || force) state.profile = await api("/api/v1/career-profile");
  return state.profile;
}

async function loadPreferences(force) {
  if (!state.preferences || force) state.preferences = await api("/api/v1/preferences");
  return state.preferences;
}

async function savePreferences(patch) {
  const next = { ...(state.preferences || {}), ...patch };
  state.preferences = await api("/api/v1/preferences", { method: "PUT", body: {
    minimumSalary: next.minimumSalary || "",
    remote: !!next.remote,
    hybrid: !!next.hybrid,
    onsite: !!next.onsite,
    onboardingComplete: !!next.onboardingComplete,
  } });
  return state.preferences;
}

async function loadResumes(force) {
  if (!state.resumes || force) state.resumes = await api("/api/v1/resumes");
  return state.resumes;
}

async function resumeText(id) {
  if (!state.resumeText.has(id)) {
    const resume = await api(`/api/v1/resumes/${encodeURIComponent(id)}`);
    state.resumeText.set(id, resume.resumeText || "");
  }
  return state.resumeText.get(id);
}

const isPro = () => state.billing && state.billing.plan === "pro";
// Interview Prep subscribers can use interview practice and nothing else.
const isInterviewOnly = () => state.billing && state.billing.plan === "interview";
const hasInterview = () => isPro() || isInterviewOnly();
// Free plan: signed in without Pro or Interview Prep (billing.free says what's left).
const isFree = () => !!(state.billing && state.billing.billingEnabled && !hasInterview());
const freeLeft = () => (state.billing && state.billing.free) || null;

// freeUpgrade is the small card shown where the Free plan stops.
function freeUpgrade(title, body) {
  const monthly = planFor("monthly");
  return h("div", { class: "card inset free-upgrade stack-sm" },
    h("div", { class: "row" }, h("span", { class: "pill pill-pro" }, "Pro"), h("h3", {}, title)),
    h("p", { class: "muted small" }, body),
    h("div", { class: "row" },
      h("button", { class: "btn btn-primary btn-sm", type: "button", onclick: () => startCheckout("monthly") },
        monthly ? subscribeLabel(monthly) : "Upgrade to Pro"),
      link("/pricing", { class: "btn btn-ghost btn-sm" }, "Compare plans")));
}

function refreshUsage() {
  const slot = document.querySelector(".sidebar-foot > div");
  if (slot) renderUsage(slot);
}

// ---------------------------------------------------------------------------
// Résumé PDF reading (in the browser, with Mozilla pdf.js)
// ---------------------------------------------------------------------------

let pdfjsPromise;
function loadPdfjs() {
  if (!pdfjsPromise) {
    pdfjsPromise = import("/vendor/pdfjs/pdf.min.mjs").then((pdfjs) => {
      pdfjs.GlobalWorkerOptions.workerSrc = "/vendor/pdfjs/pdf.worker.min.mjs";
      return pdfjs;
    });
  }
  return pdfjsPromise;
}

const MAX_RESUME_FILE_BYTES = 10 * 1024 * 1024;

async function extractResumeText(file) {
  if (file.size > MAX_RESUME_FILE_BYTES) throw new Error(`${file.name} is larger than 10 MB.`);

  if (file.type === "text/plain" || /\.txt$/i.test(file.name)) {
    return (await file.text()).trim();
  }
  if (file.type !== "application/pdf" && !/\.pdf$/i.test(file.name)) {
    throw new Error(`${file.name} isn't a PDF. Export your résumé as a PDF and try again.`);
  }

  const pdfjs = await loadPdfjs();
  const data = new Uint8Array(await file.arrayBuffer());
  const pdf = await pdfjs.getDocument({ data, isEvalSupported: false, disableFontFace: true }).promise;

  const pages = [];
  for (let number = 1; number <= Math.min(pdf.numPages, 20); number++) {
    const page = await pdf.getPage(number);
    const content = await page.getTextContent();

    const lines = [];
    let line = "";
    let lastY = null;
    let lastEnd = null;

    for (const item of content.items) {
      if (typeof item.str !== "string") continue;
      const x = item.transform[4];
      const y = item.transform[5];

      if (lastY !== null && Math.abs(y - lastY) > 2) {
        lines.push(line);
        line = "";
        lastEnd = null;
      } else if (lastEnd !== null && x - lastEnd > 1.5 && !line.endsWith(" ") && !item.str.startsWith(" ")) {
        line += " ";
      }

      line += item.str;
      lastY = y;
      lastEnd = x + (item.width || 0);

      if (item.hasEOL) {
        lines.push(line);
        line = "";
        lastY = null;
        lastEnd = null;
      }
    }
    if (line) lines.push(line);
    pages.push(lines.map((l) => l.replace(/\s+/g, " ").trim()).filter(Boolean).join("\n"));
  }

  return pages.join("\n\n").trim();
}

async function uploadResumeFiles(files) {
  const added = [];
  for (const file of files) {
    const text = await extractResumeText(file);
    const resume = await api("/api/v1/resumes", { method: "POST", body: { filename: file.name, resumeText: text } });
    state.resumeText.set(resume.id, text);
    added.push(resume);
  }
  await loadResumes(true);
  return added;
}

function resumeDropzone(onUploaded, { label = "Upload résumé PDFs", hint = "Select one or multiple files" } = {}) {
  const input = h("input", { type: "file", accept: "application/pdf,.pdf,.txt,text/plain", multiple: true, class: "visually-hidden", id: `resume-input-${Math.random().toString(36).slice(2)}` });
  const status = h("div", { class: "small muted", role: "status" });
  const zone = h("label", { class: "dropzone", for: input.id },
    h("span", { class: "dropzone-icon", "aria-hidden": "true" }, "⇪"),
    h("span", { class: "dropzone-title" }, label),
    h("span", { class: "faint small" }, hint),
  );

  const handle = async (fileList) => {
    const files = [...fileList];
    if (!files.length) return;
    zone.classList.add("busy");
    status.replaceChildren(spinnerRow(`Reading ${files.length === 1 ? files[0].name : `${files.length} files`}…`));
    try {
      const added = await uploadResumeFiles(files);
      status.replaceChildren(`Added ${added.length} résumé${added.length === 1 ? "" : "s"}.`);
      onUploaded && onUploaded(added);
    } catch (error) {
      status.replaceChildren(h("span", { class: "text-error" }, error.message));
    } finally {
      zone.classList.remove("busy");
      input.value = "";
    }
  };

  input.addEventListener("change", () => handle(input.files));
  zone.addEventListener("dragover", (event) => { event.preventDefault(); zone.classList.add("over"); });
  zone.addEventListener("dragleave", () => zone.classList.remove("over"));
  zone.addEventListener("drop", (event) => {
    event.preventDefault();
    zone.classList.remove("over");
    handle(event.dataTransfer.files);
  });

  return h("div", { class: "stack-sm" }, input, zone, status);
}

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

const routes = {
  "/": viewLanding,
  "/pricing": viewLanding,
  "/try": () => navigate(state.user ? "/app/interview" : "/signup", { replace: true }),
  "/fit-check": () => navigate(state.user ? "/app/documents" : "/signup", { replace: true }),
  "/login": () => viewAuth("login"),
  "/signup": () => viewAuth("signup"),
  "/forgot-password": viewForgotPassword,
  "/reset-password": viewResetPassword,
  "/app": () => appPage("home", viewDashboard),
  "/app/home": () => navigate("/app", { replace: true }),
  "/app/welcome": () => navigate("/app", { replace: true }),
  "/app/activate": viewActivate,
  "/app/interview": () => appPage("interview", viewMockInterview),
  "/app/documents": () => appPage("documents", viewDocuments),
  "/app/resumes": () => appPage("resumes", viewResumes),
  "/app/billing": () => appPage("billing", viewBilling),
  // Job search is retired: its pages lead to the dashboard.
  "/app/fit-check": () => navigate("/app/documents", { replace: true }),
  "/app/jobs": () => navigate("/app", { replace: true }),
  "/app/applications": () => navigate("/app", { replace: true }),
  "/app/profile": () => navigate("/app", { replace: true }),
};

function navigate(path, { replace } = {}) {
  if (replace) history.replaceState(null, "", path);
  else history.pushState(null, "", path);
  render();
}

window.addEventListener("popstate", render);

async function render() {
  document.body.classList.remove("nav-locked");
  const path = location.pathname.replace(/\/+$/, "") || "/";
  const view = routes[path];
  if (!view) {
    mount(viewNotFound());
    return;
  }
  await view();
  const anchor = location.hash ? document.getElementById(decodeURIComponent(location.hash.slice(1))) : null;
  if (anchor) {
    anchor.scrollIntoView();
  } else if (path === "/pricing") {
    const pricing = document.getElementById("pricing");
    if (pricing) pricing.scrollIntoView();
  }
}

// ---------------------------------------------------------------------------
// Marketing site
// ---------------------------------------------------------------------------

function brand() {
  return link("/", { class: "brand", "aria-label": BRAND },
    h("img", { class: "brand-logo", src: "/logo-mark.svg", alt: "", width: 42, height: 42 }),
    h("span", { class: "brand-text" }, h("span", { class: "brand-name" }, "Jane"), h("span", { class: "brand-sub" }, "Careers")));
}

// ---------------------------------------------------------------------------
// Top menu: Product, Interview Prep and Resources open a panel of links;
// on a phone the whole menu opens from the menu button.
// ---------------------------------------------------------------------------

const NAV_MENUS = [
  { label: "Product", items: [
    { title: "Job search", text: "Jobs near you, scored by how well you fit", href: "/try" },
    { title: "Résumé check", text: "What you're missing and how to fix it", href: "/try" },
    { title: "Tailored résumés", text: "Your best résumé, fitted to each job", href: "/#how" },
    { title: "Apply direct", text: "Straight to the employer's own site", href: "/#how" },
    { title: "Try it free", text: "One search, no card needed", href: "/try" },
    { title: "How Jane works", text: "Find the jobs, fix your résumé, apply", href: "/#how" },
  ] },
  { label: "Interview Prep", items: [
    { title: "Practice out loud", text: "Speak your answers. Jane listens and coaches you", href: "/#interview" },
    { title: "Interview questions by job", text: "Likely questions and sample answers", href: "/interview-questions", plain: true },
    { title: "Tell me about yourself", text: "How to answer the first question", href: "/interview-tips/tell-me-about-yourself", plain: true },
    { title: "Your greatest weakness", text: "An honest answer that still lands", href: "/interview-tips/what-is-your-greatest-weakness", plain: true },
    { title: "The STAR method", text: "Answer any \"tell me about a time\" question", href: "/interview-tips/star-method", plain: true },
    { title: "Interview Prep plan", text: "Just the interview practice", href: "/pricing" },
  ] },
  { label: "Resources", items: [
    { title: "Interview questions", text: "By job, with sample answers", href: "/interview-questions", plain: true },
    { title: "Why should we hire you?", text: "Turn it into your best answer", href: "/interview-tips/why-should-we-hire-you", plain: true },
    { title: "Questions to ask the interviewer", text: "End the interview strong", href: "/interview-tips/questions-to-ask-the-interviewer", plain: true },
    { title: "Customer service", text: "Call 646-821-2027", href: "/contact", plain: true },
    { title: "Refund policy", text: "3-day money-back guarantee", href: "/refunds", plain: true },
  ] },
];

function closeNavMenus(except) {
  document.querySelectorAll(".nav-item.open").forEach((item) => {
    if (item === except) return;
    item.classList.remove("open");
    const trigger = item.querySelector(".nav-trigger");
    if (trigger) trigger.setAttribute("aria-expanded", "false");
  });
}

document.addEventListener("click", (event) => {
  if (!event.target.closest || !event.target.closest(".nav-item")) closeNavMenus();
});
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
    closeNavMenus();
    document.querySelectorAll(".site-nav.menu-open").forEach((nav) => nav.classList.remove("menu-open"));
  }
});

function navMenuLink(item) {
  const children = [h("span", { class: "menu-link-title" }, item.title), h("span", { class: "menu-link-text" }, item.text)];
  // Guide and legal pages are separate pages, so they load in full.
  if (item.plain) return h("a", { class: "menu-link", href: item.href }, ...children);
  return link(item.href, { class: "menu-link" }, ...children);
}


function legalLinks(className) {
  return h("nav", { class: className, "aria-label": "Legal" },
    h("a", { href: "/terms" }, "Terms"),
    h("a", { href: "/privacy" }, "Privacy"),
    h("a", { href: "/refunds" }, "Refunds"),
    h("a", { href: "/contact" }, "Contact"),
  );
}

function siteFooter() {
  const column = (title, ...links) => h("div", { class: "foot-col" }, h("h2", {}, title), ...links);
  const page = (href, text) => h("a", { href }, text);
  return h("footer", { class: "site-footer foot-big" },
    h("div", { class: "container foot-grid" },
      h("div", { class: "foot-brand" }, brand(), h("p", {}, "Practice the interview before the interview.")),
      column("Product", link("/#how", {}, "How it works"), link("/#rounds", {}, "Interview types"), link("/#resume", {}, "Résumé & cover letter"), link("/pricing", {}, "Pricing")),
      column("Resources", page("/interview-questions", "Interview questions"), page("/interview-tips/tell-me-about-yourself", "Interview tips")),
      column("Customer service", page("tel:+16468212027", "646-821-2027"), page("/contact", "Contact us")),
    ),
    h("div", { class: "container foot-bottom" },
      h("span", {}, `© ${new Date().getFullYear()} ${BRAND} · Payments secured by Stripe`),
      h("nav", { class: "footer-links", "aria-label": "Legal" }, page("/terms", "Terms"), page("/privacy", "Privacy"), page("/refunds", "Refunds")),
    ),
  );
}

// ---------------------------------------------------------------------------
// Landing page: full-screen headline, then "Everything Jane does" tiles
// ---------------------------------------------------------------------------

// jdWave draws a static sound wave for the interview tile.
function jdWave(bars = 46) {
  const heights = [10, 16, 24, 34, 44, 52, 60, 40, 28, 18];
  const wave = h("div", { class: "jd-wave", "aria-hidden": "true" });
  for (let i = 0; i < bars; i++) {
    const bar = h("i", {});
    bar.style.setProperty("height", `${heights[(i * 7 + (i % 3) * 3) % heights.length]}px`);
    wave.append(bar);
  }
  return wave;
}

function jdRing(percent) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 120 120");
  svg.setAttribute("aria-hidden", "true");
  const circle = (stroke, dash) => {
    const c = document.createElementNS(ns, "circle");
    for (const [k, v] of Object.entries({ cx: 60, cy: 60, r: 50, fill: "none", stroke, "stroke-width": 8, "stroke-linecap": "round", transform: "rotate(-90 60 60)" })) c.setAttribute(k, v);
    if (dash) c.setAttribute("stroke-dasharray", dash);
    return c;
  };
  svg.append(circle("rgba(255,255,255,.1)"), circle("#fff", `${Math.round(3.14 * percent)} 314`));
  return h("div", { class: "jd-ring" }, svg, h("b", {}, `${percent}%`));
}

function jdStat(label, value, note, tone) {
  return h("div", { class: "jd-stat" }, h("span", {}, label), h("b", {}, value, " ", h("small", { class: tone }, note)));
}

// startInterviewPrep sends someone to the Interview Prep plan, or straight
// to practice when they already have access.
function startInterviewPrep() {
  if (state.user && state.billing && (isPro() || isInterviewOnly() || isFree())) {
    navigate("/app/interview");
    return;
  }
  startCheckout("interview");
}

// ---------------------------------------------------------------------------
// Homepage: Meet Jane, How Jane works (three illustrated steps), interview
// practice sample, and a note from the founder.
// ---------------------------------------------------------------------------

// svgIcon turns a fixed, trusted SVG string from this file into an element.
function svgIcon(markup) {
  const t = document.createElement("template");
  t.innerHTML = markup.trim();
  return t.content.firstElementChild;
}

const HD_ARROW = '<svg class="hd-arrow" aria-hidden="true" width="34" height="34" viewBox="0 0 24 24" fill="none" stroke="#a1a1aa" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M13 6l6 6-6 6"/></svg>';
const HD_SEARCH = '<svg aria-hidden="true" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#a1a1aa" stroke-width="2"><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/></svg>';
const HD_PERSON = '<svg aria-hidden="true" width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="#7ee39a" stroke-width="2"><circle cx="12" cy="8" r="4"/><path d="M4 21c1.5-4 4.5-6 8-6s6.5 2 8 6"/></svg>';

function hdRing(text, tone, size) {
  return h("span", { class: `hd-ring hd-ring-${tone || "white"} ${size || ""}` }, text);
}

function hdLines() {
  return h("span", { class: "hd-lines", "aria-hidden": "true" }, h("i", {}), h("i", {}), h("i", {}), h("i", {}));
}

function hdStep(num, title, text, visual) {
  return h("div", { class: "hd-step" },
    h("div", { class: "hd-step-text" },
      h("span", { class: "hd-num", "aria-hidden": "true" }, String(num)),
      h("div", {}, h("h3", {}, title), h("p", {}, text))),
    h("div", { class: "hd-card" }, visual));
}

function hdFindVisual() {
  return h("div", {},
    h("div", { class: "hd-search" }, svgIcon(HD_SEARCH), h("span", {}, "Software engineer"), h("span", { class: "hd-dot" }, "·"), h("span", {}, "New York, NY")),
    h("div", { class: "hd-job" },
      hdRing("88%", "white", "hd-ring-lg"),
      h("div", {},
        h("div", { class: "hd-job-title" }, "Software Engineer"),
        h("div", { class: "hd-muted" }, "Company · 3 miles away"),
        h("span", { class: "hd-pill-good" }, "Great match · Apply"))),
    h("p", { class: "hd-caption" }, "Your match score for every job near you"),
    h("div", { class: "hd-roles", "aria-label": "Jobs Jane searches" },
      ["Software engineer", "Registered nurse", "Teacher", "Office manager", "Sales", "Dental assistant", "Customer service"].map((role) => h("span", {}, role))));
}

function hdResume(label, score, tone, mark, good) {
  return h("div", { class: "hd-resume" },
    h("div", { class: "hd-kicker" }, label),
    h("div", { class: "hd-paper" }, hdLines(), h("div", { class: good ? "hd-good" : "hd-mid" }, mark)),
    hdRing(score, tone, "hd-ring-md"));
}

function hdFixVisual() {
  return h("div", {},
    h("p", { class: "hd-lead" }, "The job asks for ", h("b", {}, "SQL"), ". You know it, but it's not on your résumé."),
    h("div", { class: "hd-compare" },
      hdResume("Before", "72%", "gray", "✗ Missing: SQL", false),
      svgIcon(HD_ARROW),
      hdResume("After", "86%", "white", "✓ Added: SQL", true)));
}

function hdApplyVisual() {
  const pile = h("div", { class: "hd-pile", "aria-hidden": "true" });
  for (let i = 0; i < 6; i++) {
    const card = h("i", {});
    card.style.setProperty("left", `${10 + i * 7}px`);
    card.style.setProperty("top", `${6 + i * 6}px`);
    pile.append(card);
  }
  return h("div", { class: "hd-two" },
    h("div", { class: "hd-side hd-side-bad" },
      h("div", { class: "hd-kicker" }, "One-click job board"),
      pile,
      h("div", { class: "hd-side-title" }, "Lost in the pile"),
      h("div", { class: "hd-muted small" }, "Hundreds of mass applications")),
    h("div", { class: "hd-side hd-side-good" },
      h("div", { class: "hd-kicker" }, "Employer's own site"),
      h("div", { class: "hd-direct" }, h("span", { class: "hd-paper hd-paper-sm" }, hdLines()), svgIcon(HD_ARROW), h("span", { class: "hd-person" }, svgIcon(HD_PERSON))),
      h("div", { class: "hd-side-title" }, "Seen by the hiring team"),
      h("div", { class: "hd-muted small" }, "With a résumé built for that job")));
}

function hdInterviewVisual() {
  const wave = h("div", { class: "hd-wave", "aria-hidden": "true" });
  for (const height of [14, 30, 44, 24, 50, 20, 38, 16, 42, 26, 12]) {
    const bar = h("i", {});
    bar.style.setProperty("height", `${height}px`);
    wave.append(bar);
  }
  const stat = (label, value, note, tone) => h("div", { class: "hd-stat" }, h("span", {}, label), h("b", { class: tone === "mid" ? "hd-mid" : "" }, value), h("small", { class: tone === "mid" ? "hd-mid" : "hd-good" }, note));
  return h("div", {},
    h("div", { class: "hd-chips" }, h("span", {}, "Phone screen"), h("span", {}, "First round"), h("span", { class: "on" }, "Final round"), h("em", {}, "Sample")),
    h("div", { class: "hd-question" }, "“Tell me about a time you handled a difficult customer.”"),
    wave,
    h("div", { class: "hd-stats" }, stat("Pace", "148", "wpm · good"), stat("Filler words", "3", "“um”, “like”", "mid"), stat("Length", "1:42", "just right")),
    h("p", { class: "hd-tip" }, h("b", {}, "How to improve: "), "Lead with the result, then explain the steps you took."),
    h("span", { class: "hd-again" }, "Try again ↻"));
}

function janeHomeSections() {
  const stepCard = (num, title, text, visual) => h("li", { class: "card hd5-card" },
    h("span", { class: "hd-num", "aria-hidden": "true" }, String(num)),
    h("h3", {}, title), h("p", {}, text), h("div", { class: "hd5-visual" }, visual));
  const how = h("section", { class: "hd-section", id: "how" },
    h("div", { class: "container hd-wrap hd-wide" },
      h("p", { class: "hd-eyebrow" }, "How Jane works"),
      h("h2", { class: "hd-h2" }, "Jane gets your application ready."),
      h("ol", { class: "hd5" },
        stepCard(1, "Upload your résumé", "Tell Jane the job you want and your city.",
          h("span", { class: "hd5-file" }, svgIcon('<svg aria-hidden="true" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/></svg>'), "resume.pdf")),
        stepCard(2, "See your matches", "Every job near you, scored by how well you fit.",
          h("span", { class: "hd5-match" }, hdRing("92%", "white"), h("span", {}, h("b", {}, "Software Engineer"), h("small", {}, "2 mi away")))),
        stepCard(3, "Know what to change", "What will make you stand out, and what you're missing.",
          h("span", { class: "hd5-change" }, h("b", {}, "Say it clearly"), h("small", {}, "Lead with \u201cdistributed systems\u201d"))),
        stepCard(4, "Get your cover letter", "Written for the job, from your real experience.",
          h("span", { class: "hd5-letter" }, "Dear Hiring Manager,", h("br", {}), "I am applying for the Software Engineer position at Northwind Labs…")),
        stepCard(5, "Apply direct", "On the employer's own site, never a job board.",
          h("span", { class: "hd5-apply" }, "Apply on their site ↗"))),
      state.user ? null : h("p", { class: "hd-try" }, link("/try", {}, "Try it on your résumé →")),
    ));

  const practice = h("section", { class: "hd-section hd-border", id: "interview" },
    h("div", { class: "container hd-wrap" },
      h("p", { class: "hd-eyebrow" }, "Interview Prep · Practice out loud"),
      h("h2", { class: "hd-h2" }, "Walk into every interview ready."),
      h("p", { class: "hd-sub" }, "Reading a list of questions isn't practice. With Jane you speak your answers out loud, like the real interview. She listens and coaches how you say it: your pace, your filler words, how long you talk, and how to make the answer stronger."),
      h("div", { class: "hd-split" },
        h("div", { class: "hd-points" },
          h("div", {}, h("span", { class: "hd-num" }, "1"), h("div", {}, h("h3", {}, "Pick your stage"), h("p", {}, "Phone screen, first round, final round and more."))),
          h("div", {}, h("span", { class: "hd-num" }, "2"), h("div", {}, h("h3", {}, "Speak your answer out loud"), h("p", {}, "Jane asks the questions they're likely to ask, and you answer like it's the real thing."))),
          h("div", {}, h("span", { class: "hd-num" }, "3"), h("div", {}, h("h3", {}, "Get coached on how you sound"), h("p", {}, "Pace, filler words, length and how to improve. Then say it again, better."))),
          h("div", { class: "card hd-own" },
            h("div", {}, h("b", {}, "Already have an interview?"),
              h("p", {}, `Get Interview Prep on its own for ${(planFor("interview") || { displayPrice: "$29.99" }).displayPrice}/month. It's included in Pro.`)),
            h("button", { class: "btn btn-primary", type: "button", onclick: () => startCheckout("interview") }, "Get Interview Prep"))),
        h("div", { class: "hd-card" }, hdInterviewVisual())),
    ));

  const founder = h("section", { class: "hd-section hd-border" },
    h("div", { class: "container hd-founder" },
      h("p", { class: "hd-eyebrow" }, "A note from the founder"),
      h("p", { class: "hd-note" }, "“A few months ago, I was doing what everyone tells you to do, apply to as many jobs as possible. I was sending out hundreds of applications a week and hearing nothing back. The problem wasn’t effort. I didn’t know which jobs I actually had a shot at, what my résumé was missing, or how to stand out once I got the interview. So I built the tool I wish I’d had. Jane finds the jobs you’re most qualified for, shows you exactly what to fix, and helps you walk into the interview ready. Fewer applications, more offers.”"),
      h("p", { class: "hd-note" }, "Good luck, I am rooting for you."),
      h("p", { class: "hd-sign" }, "Hanna ", h("span", {}, "· Founder, Jane Careers")),
    ));

  return [how, practice, founder];
}

// Live numbers from Jane's own job database, under the hero. Every number is
// a real count (cached a few minutes on the server); if they can't load, the
// strip stays hidden.
const TRY_PLACE_KEY = "janecareers.tryPlace";

function liveJobsStrip() {
  const fmt = (n) => Number(n || 0).toLocaleString("en-US");
  const open = h("strong", {}, "—");
  const week = h("strong", {}, "—");
  const today = h("strong", {}, "—");
  const result = h("div", { class: "live-result", "aria-live": "polite" });
  const place = h("input", { type: "text", maxlength: 100, placeholder: "Your city, like Tampa, FL", "aria-label": "Your city" });
  const go = h("button", { type: "submit", class: "btn btn-primary" }, "Check");
  const strip = h("section", { class: "live-strip", hidden: true },
    h("div", { class: "container live-wrap" },
      h("p", { class: "live-label" }, h("span", { class: "live-dot", "aria-hidden": "true" }), "Live from Jane's job database"),
      h("div", { class: "live-nums" },
        h("div", {}, week, h("span", {}, "jobs posted this week")),
        h("div", {}, today, h("span", {}, "posted in the last 2 days")),
        h("div", {}, open, h("span", {}, "open jobs right now"))),
      h("form", { class: "live-form", onsubmit: async (event) => {
        event.preventDefault();
        const where = place.value.trim();
        if (!where) { place.focus(); return; }
        go.disabled = true;
        result.replaceChildren(h("p", { class: "muted small" }, "Counting…"));
        try {
          const data = await api(`/api/v1/stats/jobs?place=${encodeURIComponent(where)}`, { allowUnauthorized: true });
          if (!data.near) {
            result.replaceChildren(h("p", { class: "muted small" }, "Add your state, like “Tampa, FL”."));
          } else {
            try { sessionStorage.setItem(TRY_PLACE_KEY, where); } catch { /* ignore */ }
            const n = data.near;
            result.replaceChildren(
              h("p", { class: "live-answer" },
                h("b", {}, fmt(n.open)), ` open jobs in ${data.state}`,
                n.newToday > 0 ? [" · ", h("b", {}, fmt(n.newToday)), " posted in the last 2 days"] : n.newThisWeek > 0 ? [" · ", h("b", {}, fmt(n.newThisWeek)), " posted this week"] : null),
              state.user
                ? link("/app", { class: "btn btn-ghost" }, "See your matches →")
                : link("/try", { class: "btn btn-ghost" }, "See which ones you qualify for →"));
          }
        } catch {
          result.replaceChildren(h("p", { class: "muted small" }, "Couldn't count right now. Try again in a minute."));
        } finally {
          go.disabled = false;
        }
      } }, place, go),
      result));

  api("/api/v1/stats/jobs", { allowUnauthorized: true }).then((data) => {
    if (!data || !data.all || !data.all.open) return;
    open.textContent = fmt(data.all.open);
    week.textContent = fmt(data.all.newThisWeek);
    today.textContent = fmt(data.all.newToday);
    strip.hidden = false;
  }).catch(() => null);
  return strip;
}

// ---------------------------------------------------------------------------
// Homepage (Nov 2026 redesign): sell the need, then let people try Jane on
// their own résumé right in the hero.
// ---------------------------------------------------------------------------

const GUARANTEE_SHORT = "Interview guarantee: no interview in 30 days on Pro, your next month is free.";
const ICON_SHIELD = '<svg aria-hidden="true" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3l8 3v6c0 4.5-3.4 8.2-8 9-4.6-.8-8-4.5-8-9V6z"/><path d="M8.5 12l2.5 2.5 4.5-5"/></svg>';
const ICON_UPLOAD = '<svg aria-hidden="true" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16V4M7 9l5-5 5 5M5 20h14"/></svg>';

// A résumé picked in the homepage hero, handed to /try to run the search.
let pendingTrial = null;

function guaranteeBanner() {
  return h("div", { class: "nh-banner" }, svgIcon(ICON_SHIELD),
    h("span", {}, h("b", {}, "Interview guarantee: "), "no interview in 30 days on Pro, your next month is free. ",
      h("a", { href: "/refunds#guarantee" }, "How it works")));
}

function heroTryCard() {
  if (state.user) {
    return h("div", { class: "nh-try card" },
      h("p", { class: "nh-kicker" }, "Welcome back"),
      h("h2", {}, "Your matches are waiting"),
      h("p", { class: "muted" }, "Search jobs, get your résumé and cover letter ready, and practice interviews."),
      link("/app", { class: "btn btn-primary btn-lg btn-block" }, "Go to my account"));
  }
  if (trialUsed()) {
    return h("div", { class: "nh-try card" },
      h("p", { class: "nh-kicker" }, "Free account"),
      h("h2", {}, "Keep going with Jane"),
      h("p", { class: "muted" }, "You've used your search without an account. Sign up free for your top 5 matches on every search, plus a free cover letter."),
      link("/signup", { class: "btn btn-primary btn-lg btn-block" }, "Sign up free"),
      h("p", { class: "faint small center" }, "No card needed"));
  }
  const role = h("input", { type: "text", required: true, maxlength: 100, placeholder: "Job title you want", "aria-label": "Job title you want", autocomplete: "organization-title" });
  let rememberedPlace = "";
  try { rememberedPlace = sessionStorage.getItem(TRY_PLACE_KEY) || ""; } catch { /* ignore */ }
  const place = h("input", { type: "text", required: true, maxlength: 100, placeholder: "City, ZIP or Remote", "aria-label": "City, ZIP or Remote", value: rememberedPlace });
  const file = h("input", { id: "nh-resume", type: "file", accept: ".pdf,.txt,application/pdf,text/plain", class: "try-file-input" });
  const fileName = h("strong", {}, "Drop your résumé here");
  const fileHint = h("span", {}, "PDF · read in your browser");
  file.addEventListener("change", () => {
    const chosen = file.files && file.files[0];
    fileName.textContent = chosen ? chosen.name : "Drop your résumé here";
    fileHint.textContent = chosen ? "Ready · tap to change" : "PDF · read in your browser";
  });
  const drop = h("label", { for: "nh-resume", class: "nh-drop" }, svgIcon(ICON_UPLOAD), fileName, fileHint);
  for (const type of ["dragenter", "dragover"]) drop.addEventListener(type, (event) => { event.preventDefault(); drop.classList.add("over"); });
  for (const type of ["dragleave", "drop"]) drop.addEventListener(type, (event) => { event.preventDefault(); drop.classList.remove("over"); });
  drop.addEventListener("drop", (event) => {
    const dropped = event.dataTransfer && event.dataTransfer.files && event.dataTransfer.files[0];
    if (!dropped) return;
    const transfer = new DataTransfer();
    transfer.items.add(dropped);
    file.files = transfer.files;
    file.dispatchEvent(new Event("change"));
  });
  const error = h("p", { class: "try-error", role: "alert" });
  return h("form", { class: "nh-try card", onsubmit: (event) => {
    event.preventDefault();
    const chosen = file.files && file.files[0];
    if (!chosen) { error.textContent = "Add your résumé so Jane can check it."; return; }
    if (!role.value.trim() || !place.value.trim()) { error.textContent = "Tell Jane the job you want and where."; return; }
    pendingTrial = { role: role.value.trim(), place: place.value.trim(), file: chosen };
    navigate("/try");
  } },
    h("p", { class: "nh-kicker" }, "Try it now · no account"),
    h("h2", {}, "See your matches in 60 seconds"),
    drop, file,
    h("div", { class: "nh-fields" }, role, place),
    error,
    h("button", { type: "submit", class: "btn btn-primary btn-lg btn-block" }, "Find my jobs"),
    h("p", { class: "faint small center" }, "Your résumé is read in your browser and never sold."));
}

function nhMiniDoc(title, tag, widths) {
  return h("div", { class: "nh-doc", "aria-hidden": "true" },
    h("span", { class: "nh-doc-tag" }, tag),
    h("b", {}, title),
    ...widths.map((width) => { const line = h("i", {}); line.style.width = `${width}%`; return line; }));
}

function nhSections() {
  const how = (n, title, text) => h("li", { class: "card nh-step" }, h("span", { class: "nh-kicker" }, `Step ${n}`), h("h3", {}, title), h("p", {}, text));
  const tick = (text) => h("li", {}, text);
  const meter = (label, value, note) => h("div", { class: "nh-meter" }, h("span", {}, label), h("b", {}, value), h("small", {}, note));
  return [
    h("section", { class: "nh-section", id: "how" },
      h("div", { class: "container" },
        h("p", { class: "hd-eyebrow" }, "How it works"),
        h("h2", { class: "nh-h2" }, "From application to interview."),
        h("ol", { class: "nh-steps" },
          how(1, "Drop your résumé", "Tell Jane the job you want and where."),
          how(2, "Find jobs you can actually land", "Jane checks every opening against your résumé and tells you how strong a fit you are, and why."),
          how(3, "Apply ready", "Your résumé and cover letter, written for each job, as PDF or Word. Apply on the employer's site.")))),
    h("section", { class: "nh-section", id: "interview" },
      h("div", { class: "container nh-prep" },
        h("div", {},
          h("p", { class: "hd-eyebrow" }, "Interview Prep"),
          h("h2", { class: "nh-h2 nh-left" }, "Customizable interview prep for each company."),
          h("p", { class: "muted nh-prep-lead" }, "Pick the company and the round. Jane asks what that interviewer is likely to ask, you answer out loud, and she coaches you."),
          h("div", { class: "nh-rounds" }, ["Phone screen", "Technical", "System design", "Behavioral", "Panel", "Final round"].map((r) => h("span", { class: "nh-round" }, r))),
          h("ul", { class: "ticks nh-ticks" },
            tick("Questions for that company and that round"),
            tick("See your pace, filler words and answer length"),
            tick("Get a stronger version of your answer")),
          h("p", { class: "faint small" }, "Included with Pro, or on its own with Interview Prep.")),
        h("div", { class: "card nh-prep-card" },
          h("div", { class: "nh-card-head" }, h("p", { class: "nh-kicker" }, "Software Engineer · NVIDIA"), h("span", { class: "nh-round on" }, "System design")),
          h("p", { class: "nh-prep-q" }, "“Design a ride-hailing platform like Uber. It needs to match riders with nearby drivers in under a second, track every car in real time, and handle 10 million rides a day. Walk me through it.”"),
          h("div", { class: "nh-rec" }, h("span", { class: "nh-rec-dot" }), "Recording", h("span", { class: "nh-rec-time" }, "3:48")),
          h("div", { class: "nh-meters" },
            meter("Pace", "148", "words / min"),
            meter("Filler words", "3", "um, like"),
            meter("Length", "3:48", "good depth"))))),
  ];
}

function nhFaq() {
  const item = (question, answer) => h("details", { class: "nh-faq-item" }, h("summary", {}, question), h("p", {}, answer));
  return h("section", { class: "nh-section", id: "faq" },
    h("div", { class: "container nh-faq" },
      h("h2", { class: "nh-h2 nh-left" }, "Questions"),
      item("Does Jane apply for me?", "No. Jane gets everything ready and sends you to the employer's own site, where you apply. Employers can spot auto-applications, and yours shouldn't look like one."),
      item("How does the interview guarantee work?", "Apply to at least 10 jobs with Jane on Pro. If you don't get an interview within 30 days, email us and your next month is free."),
      item("Can I cancel?", "Yes, any time from Plan & billing, and your first payment has a 3-day money-back guarantee.")));
}

function nhFinal() {
  return h("section", { class: "nh-final" },
    h("div", { class: "container" },
      state.user
        ? link("/app", { class: "btn btn-primary btn-lg" }, "Go to my account")
        : h("button", { type: "button", class: "btn btn-primary btn-lg", onclick: () => {
          window.scrollTo({ top: 0, behavior: "smooth" });
          const input = document.querySelector(".nh-try input[type=text]");
          if (input) setTimeout(() => input.focus(), 400);
        } }, "Drop your résumé, free")));
}


// ---------------------------------------------------------------------------
// Free trial (/try): one search without an account. Upload a résumé, see
// what to fix and the 3 best matches, then unlock the rest with Pro.
// ---------------------------------------------------------------------------

const TRIAL_USED_KEY = "janecareers.trialUsed";

function trialUsed() {
  try { return localStorage.getItem(TRIAL_USED_KEY) === "1"; } catch { return false; }
}

function markTrialUsed() {
  try { localStorage.setItem(TRIAL_USED_KEY, "1"); } catch { /* private mode */ }
}

async function viewTry() {
  if (state.user === undefined) await loadUser();
  await loadPlans().catch(() => null);
  const body = h("div", { class: "try-body" });
  mount(h("div", {}, siteNav(), h("main", { class: "try-page" }, h("div", { class: "container try-wrap" }, body)), siteFooter()));
  if (state.user) {
    body.replaceChildren(trialAccountNote());
    return;
  }
  if (trialUsed()) {
    body.replaceChildren(trialUsedNote());
    return;
  }
  const handoff = pendingTrial;
  pendingTrial = null;
  renderTrialForm(body, handoff || {});
  if (handoff && handoff.file) {
    const input = body.querySelector("#try-resume");
    const form = body.querySelector(".try-form");
    try {
      const transfer = new DataTransfer();
      transfer.items.add(handoff.file);
      input.files = transfer.files;
      input.dispatchEvent(new Event("change"));
      form.requestSubmit();
    } catch { /* older browser: the form is filled in, they press the button */ }
  }
}

function trialAccountNote() {
  return h("div", { class: "try-head" },
    h("h1", {}, "You're already in."),
    h("p", { class: "lead" }, "Search jobs, check your résumé and practice interviews from your account."),
    link("/app", { class: "btn btn-primary btn-lg" }, "Go to my account"));
}

function trialUsedNote() {
  return h("div", { class: "try-head" },
    h("h1", {}, "You've used your free search."),
    h("p", { class: "lead" }, "Get Pro to see every match near you, a tailored résumé for each job, and interview practice."),
    h("button", { class: "btn btn-primary btn-lg", onclick: () => startCheckout("monthly") }, "Get Pro"),
    h("p", { class: "faint small" }, "Cancel anytime · 3-day money-back guarantee"));
}

function renderTrialForm(body, saved = {}) {
  const role = h("input", { id: "try-role", type: "text", required: true, maxlength: 100, autocomplete: "organization-title", placeholder: "Software engineer, nurse, teacher…", value: saved.role || "" });
  let rememberedPlace = "";
  try { rememberedPlace = sessionStorage.getItem(TRY_PLACE_KEY) || ""; } catch { /* ignore */ }
  const place = h("input", { id: "try-place", type: "text", required: true, maxlength: 100, placeholder: "New York, NY or 10001", value: saved.place || rememberedPlace });
  const file = h("input", { id: "try-resume", type: "file", accept: ".pdf,.txt,application/pdf,text/plain", class: "try-file-input" });
  const fileName = h("span", { class: "try-file-name" }, "Upload your résumé");
  const fileHint = h("span", { class: "try-file-hint" }, "PDF, up to 10 MB");
  file.addEventListener("change", () => {
    const chosen = file.files && file.files[0];
    fileName.textContent = chosen ? chosen.name : "Upload your résumé";
    fileHint.textContent = chosen ? "Ready · tap to change" : "PDF, up to 10 MB";
  });
  const error = h("p", { class: "try-error", role: "alert" });
  const submit = h("button", { type: "submit", class: "btn btn-primary btn-lg btn-block" }, "Show my matches");

  const form = h("form", { class: "try-form", onsubmit: async (event) => {
    event.preventDefault();
    error.textContent = "";
    const chosen = file.files && file.files[0];
    if (!role.value.trim() || !place.value.trim()) { error.textContent = "Tell Jane the job you want and your city."; return; }
    if (!chosen) { error.textContent = "Upload your résumé so Jane can check it."; return; }
    submit.disabled = true;
    let resumeText;
    try {
      resumeText = await extractResumeText(chosen);
    } catch (problem) {
      error.textContent = problem.message || "Jane couldn't read that file. Try a PDF.";
      submit.disabled = false;
      return;
    }
    const request = { targetRole: role.value.trim(), location: place.value.trim(), resumeText };
    body.replaceChildren(h("div", { class: "try-loading" },
      spinnerRow(`Jane is reading your résumé and scoring ${request.targetRole} jobs near ${request.location}…`)));
    try {
      const result = await api("/api/v1/trial/search", { method: "POST", body: request, allowUnauthorized: true });
      markTrialUsed();
      renderTrialResults(body, request, result);
    } catch (problem) {
      if (problem.code === "trial_used") { markTrialUsed(); body.replaceChildren(trialUsedNote()); return; }
      renderTrialForm(body, { role: request.targetRole, place: request.location });
      const again = body.querySelector(".try-error");
      if (again) again.textContent = problem.message || "Something went wrong. Try again.";
    }
  } },
    h("div", { class: "field" }, h("label", { for: "try-role" }, "Job you want"), role),
    h("div", { class: "field" }, h("label", { for: "try-place" }, "City or ZIP"), place),
    h("div", { class: "field" },
      h("span", { class: "try-label" }, "Your résumé"),
      h("label", { for: "try-resume", class: "try-file" },
        svgIcon('<svg aria-hidden="true" width="30" height="30" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/><path d="M12 17v-6M9 14l3-3 3 3"/></svg>'),
        h("span", { class: "try-file-text" }, fileName, fileHint)),
      file),
    error,
    submit,
    h("p", { class: "faint small center" }, "One free search. Your résumé is read in your browser and isn't saved."),
  );

  body.replaceChildren(
    h("div", { class: "try-head" },
      h("span", { class: "try-chip" }, "Free · no card needed"),
      h("h1", {}, "Try Jane on one search"),
      h("p", { class: "lead" }, "Upload your résumé. Jane tells you what to fix and finds the jobs you're most qualified for.")),
    form);
}

function trialRing(percent, size) {
  return h("span", { class: `hd-ring hd-ring-white ${size || "hd-ring-md"}` }, `${percent}%`);
}

// The matches past the free three: real scores, blurred titles, and the
// count of everything else Jane found. Seeing them is what makes people unlock.
function trialLockedList(locked, more, total, monthly) {
  const lockIcon = '<svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>';
  const rows = locked.map((match) => h("div", { class: "try-lock-row", "aria-hidden": "true" },
    trialRing(match.matchPercentage),
    h("div", { class: "try-lock-text" },
      h("span", { class: "try-blur" }, match.title),
      h("span", { class: "try-blur try-blur-sub" }, "Company name · 4 miles away")),
    svgIcon(lockIcon)));
  const hidden = more - locked.length;
  return h("section", { class: "card try-lockbox" },
    h("div", { class: "try-lock-head" },
      h("span", { class: "try-chip" }, "Locked"),
      h("h2", {}, `${more} more ${more === 1 ? "job matches" : "jobs match"} you`),
      h("p", { class: "muted small" }, "Jane already scored them. Unlock to see who's hiring and apply on their site.")),
    h("div", { class: "try-lock-list" }, rows,
      hidden > 0 ? h("p", { class: "try-lock-more" }, `+ ${hidden} more`) : null,
      h("div", { class: "try-lock-cover" },
        h("button", { class: "btn btn-primary btn-lg", onclick: () => startCheckout("monthly") }, `Unlock all ${total} matches`),
        h("p", { class: "faint small" }, `${monthly.displayPrice}/month · Cancel anytime · 3-day money-back guarantee`),
        state.user ? null : h("p", { class: "small" }, "Or ", link("/signup", {}, "sign up free"), " for your top 5 matches on every search."))));
}

// "Email me new matches every morning": turns a one-time trial into a
// daily reason to come back. Jane emails a confirmation link first.
function trialAlertBox(request) {
  const email = h("input", { type: "email", required: true, maxlength: 254, autocomplete: "email", placeholder: "you@email.com", "aria-label": "Your email" });
  const submit = h("button", { type: "submit", class: "btn btn-primary" }, "Send me matches");
  const note = h("p", { class: "try-error", role: "alert" });
  const box = h("section", { class: "card try-alert" });
  const form = h("form", { class: "try-alert-form", onsubmit: async (event) => {
    event.preventDefault();
    note.textContent = "";
    const address = email.value.trim();
    if (!address) { email.focus(); return; }
    submit.disabled = true;
    try {
      const reply = await api("/api/v1/alerts", { method: "POST", allowUnauthorized: true,
        body: { email: address, targetRole: request.targetRole, location: request.location, resumeText: request.resumeText } });
      box.replaceChildren(
        h("p", { class: "try-alert-icon", "aria-hidden": "true" }, "✓"),
        h("h2", {}, reply.status === "active" ? "You're already set." : "Check your email."),
        h("p", { class: "muted" }, reply.status === "active"
          ? `Jane already emails you new ${request.targetRole} matches every morning.`
          : `Tap the link Jane sent to ${address} and your first matches arrive right away.`));
    } catch (problem) {
      note.textContent = problem.message || "Couldn't sign you up. Try again.";
      submit.disabled = false;
    }
  } }, email, submit);
  box.append(
    h("span", { class: "try-chip" }, "Free"),
    h("h2", {}, "Get new matches every morning"),
    h("p", { class: "muted" }, `New ${request.targetRole} jobs near ${request.location} post every day. Jane scores them against your résumé and emails you the best ones before everyone else applies.`),
    form, note,
    h("p", { class: "faint small" }, "To score new jobs, Jane keeps your résumé with this email. Unsubscribe anytime and it's deleted."));
  return box;
}

function renderTrialResults(body, request, result) {
  const total = result.total || 0;
  const matches = result.matches || [];
  const monthly = planFor("monthly") || { displayPrice: "$49.99" };

  if (!total) {
    body.replaceChildren(h("div", { class: "try-head" },
      h("h1", {}, "No matches yet"),
      h("p", { class: "lead" }, `Jane didn't find ${request.targetRole} openings she could score near ${request.location} today. Try a broader job title or a nearby city.`),
      h("button", { class: "btn btn-primary btn-lg", onclick: () => renderTrialForm(body, { role: request.targetRole, place: request.location }) }, "Search again")));
    try { localStorage.removeItem(TRIAL_USED_KEY); } catch { /* ignore */ }
    return;
  }

  const more = total - matches.length;
  const cards = matches.map((match, index) => {
    const meta = [match.company, distanceText(match)].filter(Boolean).join(" · ");
    return h("button", { type: "button", class: `card try-pick${index === 0 ? " try-pick-top" : ""}`, onclick: () => renderTrialJob(body, request, result, index) },
      trialRing(match.matchPercentage),
      h("span", { class: "try-pick-text" },
        h("b", {}, match.title),
        h("span", { class: "muted small" }, meta),
        h("span", { class: "small try-pick-note" }, match.matchLevel || "Good match")),
      svgIcon('<svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>'));
  });

  body.replaceChildren(
    trialSteps(2),
    h("div", { class: "try-head try-head-sm" },
      h("h1", {}, total === 1 ? "You match 1 job near you." : `You match ${total} jobs near you.`),
      h("p", { class: "muted" }, `${request.targetRole} · ${request.location} · best first`)),
    h("p", { class: "try-hint" }, "Pick a job. Jane shows you what to change, writes your cover letter, and sends you to the employer to apply."),
    h("div", { class: "stack" }, cards),
    more > 0 ? trialLockedList(result.locked || [], more, total, monthly) : null,
    h("p", { class: "faint small center" }, "Every apply link goes straight to the employer, never a job board."),
  );
  window.scrollTo(0, 0);
}

// trialSteps is the 5-step progress bar: upload, matches, what to change,
// cover letter, apply.
function trialSteps(current) {
  return h("div", { class: "try-steps", "aria-label": `Step ${current} of 5` },
    [1, 2, 3, 4, 5].map((step) => h("span", { class: step <= current ? "on" : "" })));
}

// renderTrialJob is one free match: what to change, what's missing, a
// cover letter preview, and the employer's apply link.
function renderTrialJob(body, request, result, index) {
  const match = (result.matches || [])[index];
  if (!match) return;
  const monthly = planFor("monthly") || { displayPrice: "$49.99" };
  const applyURL = safeUrl(match.applyURL || "");
  const partial = match.partial || [];
  const missing = match.missing || [];
  const strong = match.strong || [];

  const changes = partial.map((text) => h("li", {},
    h("b", {}, text),
    h("span", { class: "muted small" }, "Your résumé shows some of this. Say it plainly, with a real example, so they can't miss it.")));
  const gaps = missing.map((text) => h("li", {},
    h("b", {}, text),
    h("span", { class: "muted small" }, classifyGapClient({ text, category: "other", importance: "required" }, false).advice)));

  const letterSlot = h("div", { class: "try-letter" }, spinnerRow("Jane is writing your cover letter for this job…"));

  body.replaceChildren(
    h("button", { type: "button", class: "btn btn-ghost btn-sm try-back", onclick: () => renderTrialResults(body, request, result) }, "← All matches"),
    trialSteps(3),
    h("div", { class: "try-job-top" },
      trialRing(match.matchPercentage, "hd-ring-lg"),
      h("div", { class: "stack-xs" },
        h("h1", {}, match.title),
        h("p", { class: "muted" }, [match.company, match.location, distanceText(match)].filter(Boolean).join(" · ")))),

    h("section", { class: "card try-section" },
      h("p", { class: "try-kicker" }, "3 · What to change"),
      changes.length || gaps.length ? null : h("p", {}, "Your résumé already covers what this job asks for. Apply early."),
      changes.length ? h("div", { class: "stack-sm" }, h("h2", { class: "try-h2" }, "Change these to stand out"), h("ol", { class: "try-fixes" }, changes)) : null,
      gaps.length ? h("div", { class: "stack-sm" }, h("h2", { class: "try-h2" }, "You're missing"), h("ol", { class: "try-fixes try-gaps" }, gaps),
        h("p", { class: "faint small" }, "If you've done it, add it. If you haven't, don't. You can still apply.")) : null,
      strong.length ? h("p", { class: "try-strong" }, h("b", {}, "Already strong: "), strong.join(" · ")) : null),

    h("section", { class: "card try-section" },
      h("p", { class: "try-kicker" }, "4 · Your cover letter"),
      letterSlot),

    h("section", { class: "card try-section" },
      h("p", { class: "try-kicker" }, "5 · Apply"),
      h("p", { class: "muted small" }, `Apply on ${match.company || "the employer"}'s own site, never a job board.`),
      applyURL ? h("a", { class: "btn btn-primary btn-lg btn-block", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, `Apply on ${match.company || "the employer"}'s site ↗`) : null),

    h("p", { class: "faint small center" }, h("a", { href: "#", onclick: (event) => { event.preventDefault(); renderTrialResults(body, request, result); } }, "See your other matches")),
  );
  window.scrollTo(0, 0);
  loadTrialLetter(letterSlot, result.token, index, match, monthly);
}

// loadTrialLetter shows the start of the cover letter Jane wrote; the rest
// unlocks with Pro.
async function loadTrialLetter(slot, token, index, match, monthly) {
  if (!token) {
    slot.replaceChildren(trialLetterLocked(null, match, monthly));
    return;
  }
  try {
    const letter = await api("/api/v1/trial/cover-letter", { method: "POST", body: { token, index }, allowUnauthorized: true });
    slot.replaceChildren(trialLetterLocked(letter.preview, match, monthly));
  } catch {
    slot.replaceChildren(trialLetterLocked(null, match, monthly));
  }
}

function trialLetterLocked(preview, match, monthly) {
  const paper = h("div", { class: "try-letter-paper" },
    preview ? preview.split("\n\n").map((part) => h("p", {}, part)) : h("p", {}, "Dear Hiring Manager,"),
    h("div", { class: "try-letter-fade", "aria-hidden": "true" },
      [92, 100, 86, 97, 64, 0, 95, 88, 40].map((width) => width ? h("i", { style: `width:${width}%` }) : h("br", {}))));
  return h("div", { class: "stack-sm" },
    h("p", { class: "muted small" }, `Written for ${match.title}${match.company ? ` at ${match.company}` : ""}, only from your real experience.`),
    paper,
    h("button", { class: "btn btn-primary btn-lg btn-block", type: "button", onclick: () => startCheckout("monthly") }, "Unlock your cover letter"),
    h("p", { class: "faint small center" }, `Pro · ${monthly.displayPrice}/month · a cover letter and updated résumé for every job · 3-day money-back guarantee`));
}

// ---------------------------------------------------------------------------
// Plans, paywall, and checkout
// ---------------------------------------------------------------------------

function planFor(key) {
  return ((state.plans && state.plans.plans) || []).find((plan) => plan.key === key);
}

function trialAvailable() {
  return !state.billing || state.billing.trialEligible !== false;
}

function subscribeLabel(plan) {
  return plan.trialDays && trialAvailable() ? `Start ${plan.trialDays}-day free trial` : `Get Pro`;
}

function checkoutNote() {
  const monthly = planFor("monthly");
  return h("p", { class: "legal-note" },
    monthly && monthly.trialDays
      ? `Pro Monthly is free for ${monthly.trialDays} days, then ${monthly.displayPrice}/month until you cancel. Cancel before the trial ends and you won't be charged. `
      : "Subscriptions renew automatically until you cancel. ",
    "3-day money-back guarantee on your first payment. See our ",
    h("a", { href: "/terms" }, "Terms"), " and ", h("a", { href: "/refunds" }, "Refund Policy"), ".");
}

// planCards renders the personal plans (Interview Prep, Pro Monthly,
// Sprint), plus the school license on the full pricing page.
function planCards(plansInfo, { compact = false, include = ["monthly", "interview"] } = {}) {
  const enabled = plansInfo.billingEnabled;
  const interview = planFor("interview");
  const monthly = planFor("monthly") || { key: "monthly", name: "Pro", displayPrice: "$39.99", interval: "month" };
  const sprint = planFor("sprint") || { key: "sprint", name: "Job Search Sprint", displayPrice: "$39", interval: "90 days" };
  const school = planFor("school") || { key: "school", name: "School License", displayPrice: "$1,000", interval: "year" };

  const proFeatures = [
    h("strong", {}, "Unlimited mock interviews*"),
    "Questions built from the job posting",
    "Full loop and final-round practice",
    "Unlimited tailored résumés and cover letters",
    "Download as PDF or Word",
  ];
  const ticks = (items) => h("ul", { class: "ticks" }, items.map((f) => h("li", {}, f)));

  // Built by key, then returned in the order `include` lists them.
  const byKey = {};
  if (include.includes("free")) {
    byKey.free = (h("div", { class: "card price-card" },
      h("div", {}, h("h3", {}, "Free"), h("p", { class: "muted small" }, "Try a mock interview")),
      h("div", { class: "price" }, "$0", " ", h("small", {}, "/ forever")),
      compact ? null : ticks(["1 mock interview, 3 graded answers", "Pace, filler words and length", "A stronger version of each answer"]),
      state.user
        ? link("/app/interview", { class: "btn btn-block" }, "Start practicing")
        : link("/signup", { class: "btn btn-block" }, "Start free"),
      h("p", { class: "faint small center" }, "No card needed"),
    ));
  }
  if (include.includes("interview")) {
    const plan = interview || { key: "interview", name: "Prep", displayPrice: "$29.99", interval: "month" };
    byKey.interview = (h("div", { class: "card price-card" },
      h("div", {}, h("h3", {}, plan.name), h("p", { class: "muted small" }, "For one upcoming interview")),
      h("div", { class: "price" }, plan.displayPrice, " ", h("small", {}, `/ ${plan.interval}`)),
      compact ? null : ticks(["40 graded answers a month", "Every interview type, any company", "A stronger answer for every question", "3 tailored résumés and 3 cover letters a month"]),
      h("button", { class: "btn btn-block", disabled: !enabled || !interview, onclick: () => startCheckout("interview") },
        enabled && interview ? `Get ${plan.name}` : "Coming soon"),
    ));
  }
  if (include.includes("monthly")) {
    byKey.monthly = (h("div", { class: "card price-card featured" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, monthly.name), h("p", { class: "muted small" }, "For your whole job search")),
        h("span", { class: "pill pill-pro" }, "Most popular")),
      h("div", { class: "price" }, monthly.displayPrice, " ", h("small", {}, `/ ${monthly.interval}`)),
      compact ? null : ticks(proFeatures),
      h("button", { class: "btn btn-primary btn-block", disabled: !enabled, onclick: () => startCheckout("monthly") },
        enabled ? subscribeLabel(monthly) : "Coming soon"),
    ));
  }
  if (include.includes("sprint")) {
    byKey.sprint = (h("div", { class: "card price-card" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, sprint.name), h("p", { class: "muted small" }, `One payment, ${sprint.interval} of Pro`)),
        h("span", { class: "pill pill-pro" }, "Best value")),
      h("div", { class: "price" }, sprint.displayPrice, " ", h("small", {}, "one-time")),
      compact ? null : ticks([`Everything in Pro for ${sprint.interval}`, "Interview Prep included", "Doesn't renew"]),
      h("button", { class: "btn btn-block", disabled: !enabled, onclick: () => startCheckout("sprint") },
        enabled ? "Get the Sprint" : "Coming soon"),
    ));
  }

  if (!compact && include.includes("school")) {
    byKey.school = (h("div", { class: "card school-row" },
      h("div", {}, h("h3", {}, "Schools & career programs"),
        h("p", { class: "muted small" }, `Pro for every student with a school email · ${school.displayPrice}/${school.interval} · free 2-week pilot`)),
      state.user
        ? link("/app/billing", { class: "btn" }, "Get a school license")
        : link("/signup", { class: "btn" }, "Get a school license"),
    ));
  }

  return include.filter((key) => byKey[key]).map((key) => byKey[key]);
}

// lifetimeOffer is the limited-time lifetime deal: one payment, Pro for
// life. It shows only while the server offers the "lifetime" plan, so
// ending the offer is a server change.
// isLifetime: Lifetime Pro is a pass that runs for a century.
function isLifetime(billing) {
  return Boolean(billing && billing.passExpiresAt && new Date(billing.passExpiresAt).getFullYear() >= 2100);
}

function lifetimeOffer(plansInfo) {
  const ticks = (items) => h("ul", { class: "ticks" }, items.map((f) => h("li", {}, f)));
  const plan = planFor("lifetime");
  if (!plan) return null;
  const enabled = Boolean(plansInfo && plansInfo.billingEnabled);
  return h("div", { class: "card lifetime" },
    h("div", { class: "lifetime-copy" },
      h("span", { class: "pill pill-pro" }, "Limited time offer"),
      h("h3", {}, "Lifetime Pro"),
      h("p", { class: "muted" }, "Everything in Pro for every interview you'll ever have. Pay once."),
      ticks(["Everything in Pro, forever", "Every interview type", "Every new feature we ship"])),
    h("div", { class: "lifetime-buy" },
      h("div", { class: "price" }, plan.displayPrice, " ", h("small", {}, "one-time")),
      h("p", { class: "faint small lifetime-was" }, "Pro is ", (planFor("monthly") || {}).displayPrice || "", "/month"),
      h("button", { class: "btn btn-primary btn-block", disabled: !enabled, onclick: () => startCheckout("lifetime") },
        enabled ? "Claim lifetime access" : "Coming soon"),
      h("p", { class: "faint small center" }, "3-day money-back guarantee")));
}

function pricingSection(plansInfo, { full = false } = {}) {
  return h("section", { class: "section ip-pricing", id: "pricing" },
    h("div", { class: "container" },
      h("p", { class: "ip-label center" }, "Pricing"),
      h("h2", { class: "section-title" }, "Pick how much you'll practice."),
      h("p", { class: "section-sub" }, "Cancel anytime. 3-day money-back guarantee."),
      lifetimeOffer(plansInfo),
      h("div", { class: "pricing-three" }, planCards(plansInfo, { include: ["free", "interview", "monthly"] })),
      h("p", { class: "faint small center" }, "*Fair use: up to 300 graded answers a month."),
      full ? comparePlans() : null,
      full ? h("div", { class: "section-gap" }, checkoutNote())
        : h("p", { class: "try-line" }, link("/pricing", {}, "Compare plans")),
    ),
  );
}

// comparePlans: what each plan includes, row by row.
function comparePlans() {
  const rows = [
    ["Mock interviews", "1", "Unlimited", "Unlimited*"],
    ["Graded answers", "3 in total", "40 a month", "Unlimited*"],
    ["Interview types", "All", "All", "All"],
    ["Questions from the job posting", "✓", "✓", "✓"],
    ["Pace, filler words and length", "✓", "✓", "✓"],
    ["A stronger version of each answer", "✓", "✓", "✓"],
    ["Tailored résumés", "–", "3 a month", "Unlimited*"],
    ["Cover letters", "–", "3 a month", "Unlimited*"],
    ["PDF and Word downloads", "–", "✓", "✓"],
  ];
  const cell = (value, strong) => h("td", { class: value === "–" ? "none" : strong ? "strong" : "" }, value);
  return h("div", { class: "compare-wrap" },
    h("h3", {}, "Compare plans"),
    h("div", { class: "card compare-card" },
      h("table", { class: "compare-table" },
        h("thead", {}, h("tr", {}, h("th", { scope: "col" }, "Feature"), h("th", { scope: "col" }, "Free"), h("th", { scope: "col" }, "Prep"), h("th", { scope: "col" }, "Pro"))),
        h("tbody", {}, rows.map(([name, free, prep, pro]) => h("tr", {}, h("th", { scope: "row" }, name), cell(free), cell(prep), cell(pro, true)))))));
}

// paywall is shown where a feature needs a plan.
function paywall(title, body) {
  const plansInfo = state.plans || { billingEnabled: false };
  if (isInterviewOnly()) return upgradeFromInterview(title, body);
  return h("div", { class: "card stack paywall" },
    h("div", { class: "stack-sm" }, h("h3", {}, title), h("p", { class: "muted" }, body)),
    h("div", { class: "pricing-one" }, planCards(plansInfo, { compact: true, include: ["monthly"] })),
    plansInfo.billingEnabled ? checkoutNote() : null,
  );
}

// openBillingPortal opens Stripe's billing page, where Interview Prep
// subscribers switch to Pro without paying for two plans.
async function openBillingPortal(button) {
  if (button) button.disabled = true;
  try {
    const { url } = await api("/api/v1/billing/portal", { method: "POST" });
    window.location.assign(url);
  } catch (error) {
    if (button) button.disabled = false;
    toast(error.message, true);
  }
}

// upgradeFromInterview is the paywall an Interview Prep subscriber sees on a
// Pro feature.
function upgradeFromInterview(title, body) {
  const monthly = planFor("monthly");
  const button = h("button", { class: "btn btn-primary", onclick: () => openBillingPortal(button) }, "Switch to Pro");
  return h("div", { class: "card stack paywall" },
    h("div", { class: "stack-sm" },
      h("span", { class: "pill pill-pro" }, "Part of Pro"),
      h("h3", {}, title || "This is part of Pro"),
      h("p", { class: "muted" }, body || "Your Prep plan includes 40 graded answers a month."),
      h("p", { class: "muted small" }, `Switch to Pro${monthly ? ` (${monthly.displayPrice}/${monthly.interval})` : ""} for unlimited mock interviews, résumés and cover letters.`)),
    h("div", { class: "row" }, button, link("/app/interview", { class: "btn" }, "Back to interview practice")),
  );
}

async function startCheckout(planKey, extra = {}) {
  if (!state.user) {
    sessionStorage.setItem("janecareers.pendingPlan", planKey);
    navigate("/signup");
    return;
  }
  try {
    const { url } = await api("/api/v1/billing/checkout", { method: "POST", body: { plan: planKey, ...extra } });
    window.location.assign(url);
  } catch (error) {
    if (error.code === "switch_in_portal") {
      toast("Switch your Prep plan to Pro in the billing page that opens.");
      openBillingPortal();
      return;
    }
    if (error.code === "already_subscribed") {
      toast(error.message);
      navigate("/app/billing");
      return;
    }
    if (error.code === "email_unverified") {
      toast("Confirm your school email first. Check your inbox for the link.", true);
      navigate("/app/billing");
      return;
    }
    toast(error.message, true);
  }
}

function viewNotFound() {
  return h("div", {}, siteNav(),
    h("div", { class: "auth-wrap" },
      h("div", { class: "empty" }, h("h3", {}, "Page not found"), link("/", { class: "btn" }, "Go home"))));
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

async function viewAuth(mode) {
  if (state.user === undefined) await loadUser();
  if (state.user) {
    navigate("/app", { replace: true });
    return;
  }

  const isSignup = mode === "signup";
  const error = h("div", { class: "form-error hidden", role: "alert" });
  const submit = h("button", { class: "btn btn-primary btn-block", type: "submit" }, isSignup ? "Create account" : "Sign in");

  const form = h("form", { class: "stack", novalidate: true, onsubmit: async (event) => {
    event.preventDefault();
    error.classList.add("hidden");
    submit.disabled = true;

    const data = new FormData(form);
    const body = { email: data.get("email"), password: data.get("password") };
    if (isSignup) {
      body.firstName = data.get("firstName") || "";
      body.lastName = data.get("lastName") || "";
      body.phone = data.get("phone") || "";
    }

    try {
      const session = await api(isSignup ? "/api/v1/auth/signup" : "/api/v1/auth/login", {
        method: "POST", body, allowUnauthorized: true,
      });
      resetState();
      state.user = session.user;

      const pendingPlan = sessionStorage.getItem("janecareers.pendingPlan");
      if (pendingPlan) {
        sessionStorage.removeItem("janecareers.pendingPlan");
        await startCheckout(pendingPlan);
        return;
      }
      navigate(isSignup ? "/app/activate" : "/app");
    } catch (err) {
      const extra = [];
      if (err.code === "account_exists") {
        extra.push(" ", link("/login", {}, "Sign in"), " · ", link("/forgot-password", {}, "Reset password"));
      } else if (!isSignup && err.status === 401) {
        extra.push(" ", link("/forgot-password", {}, "Reset your password"));
      }
      error.replaceChildren(err.message, ...extra);
      error.classList.remove("hidden");
      submit.disabled = false;
    }
  } },
    isSignup ? h("div", { class: "field-row" },
      h("label", { class: "field" }, "First name", h("input", { type: "text", name: "firstName", autocomplete: "given-name", maxlength: 60, required: true })),
      h("label", { class: "field" }, "Last name", h("input", { type: "text", name: "lastName", autocomplete: "family-name", maxlength: 60, required: true }))) : null,
    h("label", { class: "field" }, "Email", h("input", { type: "email", name: "email", autocomplete: "email", required: true })),
    isSignup ? h("label", { class: "field" }, "Phone number", h("input", { type: "tel", name: "phone", autocomplete: "tel", inputmode: "tel", placeholder: "(555) 555-5555", required: true })) : null,
    h("label", { class: "field" }, "Password",
      h("input", { type: "password", name: "password", autocomplete: isSignup ? "new-password" : "current-password", required: true, minlength: isSignup ? 8 : undefined }),
      isSignup ? h("span", { class: "faint small" }, "At least 8 characters.") : link("/forgot-password", { class: "faint small" }, "Forgot password?"),
    ),
    error,
    submit,
    isSignup ? h("p", { class: "legal-note" }, "By creating an account you agree to our ",
      h("a", { href: "/terms" }, "Terms"), " and ", h("a", { href: "/privacy" }, "Privacy Policy"), ".") : null,
  );

  mount(h("div", {}, siteNav(),
    h("div", { class: "auth-wrap" },
      h("div", { class: "card auth-card stack" },
        h("div", { class: "stack-sm" },
          h("h1", {}, isSignup ? "Create your account" : "Welcome back"),
          h("p", { class: "muted" }, isSignup ? "Create your account, then pick a plan to start. 3-day money-back guarantee." : "Sign in to continue your search."),
        ),
        form,
        h("p", { class: "muted small" },
          isSignup ? "Already have an account? " : `New to ${BRAND}? `,
          link(isSignup ? "/login" : "/signup", {}, isSignup ? "Sign in" : "Create an account"),
        ),
      ),
    ),
  ));
}

async function logout() {
  try { await api("/api/v1/auth/logout", { method: "POST", allowUnauthorized: true }); } catch { /* ignore */ }
  resetState();
  navigate("/");
}

// ---------------------------------------------------------------------------
// Onboarding: Welcome → Résumés → About you → Work preferences → Ready
// ---------------------------------------------------------------------------

async function viewOnboarding() {
  if (state.user === undefined) await loadUser();
  if (!state.user) {
    navigate("/login", { replace: true });
    return;
  }

  await Promise.all([loadBilling(), loadPlans()]);
  if (needsActivation()) {
    navigate("/app/activate", { replace: true });
    return;
  }

  const shell = h("div", { class: "onboarding" });
  mount(h("div", {}, h("header", { class: "site-nav" }, h("div", { class: "container" }, brand(),
    h("button", { class: "btn btn-ghost btn-sm", onclick: () => finishOnboarding(false) }, "Skip setup"))), shell));

  shell.replaceChildren(spinnerRow("Loading…"));
  await Promise.all([loadProfile(true), loadPreferences(true), loadResumes(true)]);

  const STEPS = ["Résumé", "About you", "Preferences", "Ready"];
  let step = 0;
  const draft = {
    headline: state.profile.headline || "",
    summary: state.profile.summary || "",
    targetRole: state.profile.targetRole || "",
    location: state.profile.location || "",
    evidence: (state.profile.evidence || []).map((e) => ({ category: e.category, text: e.text })),
    minimumSalary: state.preferences.minimumSalary || "",
    remote: !!state.preferences.remote,
    hybrid: !!state.preferences.hybrid,
    onsite: !!state.preferences.onsite,
  };

  const stepper = () => h("ol", { class: "stepper", "aria-label": "Setup progress" },
    STEPS.map((label, index) => h("li", { class: index === step ? "active" : index < step ? "done" : "", "aria-current": index === step ? "step" : undefined }, label)));

  const go = (next) => { step = next; draw(); };

  const draw = () => {
    const body = [stepResume, stepAbout, stepPreferences, stepReady][step]();
    shell.replaceChildren(h("div", { class: "container narrow stack" }, stepper(), body));
    window.scrollTo(0, 0);
  };

  // Step 1: upload résumés
  const stepResume = () => {
    const list = h("div", { class: "stack-sm" });
    const drawList = () => list.replaceChildren(...(state.resumes || []).map((r) =>
      h("div", { class: "resume-row" }, h("span", { class: "resume-name" }, "📄 ", r.filename), r.isPrimary ? h("span", { class: "pill" }, "Default") : null)));
    drawList();

    const next = h("button", { class: "btn btn-primary", disabled: !(state.resumes || []).length, onclick: async () => {
      next.disabled = true;
      const primary = (state.resumes || []).find((r) => r.isPrimary) || state.resumes[0];
      try {
        const prefill = await api(`/api/v1/resumes/${encodeURIComponent(primary.id)}/profile-draft`);
        mergeDraft(draft, prefill, true);
      } catch (error) {
        toast(error.message, true);
      }
      go(1);
    } }, "Continue");

    return h("div", { class: "card stack" },
      h("div", { class: "stack-sm" },
        h("h1", { class: "onboarding-title" }, "Hi, I'm Jane. Let's see your résumé."),
        h("p", { class: "muted" }, "Upload one or more résumés. I read them right in your browser and build your profile from them. Targeting different kinds of roles? Add a version for each."),
      ),
      resumeDropzone(() => { drawList(); next.disabled = false; }),
      list,
      h("div", { class: "row-between" },
        h("button", { class: "btn btn-ghost", onclick: () => go(1) }, "I'll type my profile instead"),
        next,
      ),
    );
  };

  // Step 2: review the profile built from the résumé
  const stepAbout = () => {
    const editor = profileEditor(draft);
    return h("div", { class: "stack" },
      h("div", { class: "card stack-sm" },
        h("h1", { class: "onboarding-title" }, "About you"),
        h("p", { class: "muted" }, "Here's what Jane picked up from your résumé. Add anything she missed. The more she knows, the sharper your matches."),
      ),
      editor.node,
      h("div", { class: "row-between" },
        h("button", { class: "btn btn-ghost", onclick: () => { Object.assign(draft, editor.read()); go(0); } }, "Back"),
        h("button", { class: "btn btn-primary", onclick: () => { Object.assign(draft, editor.read()); go(2); } }, "Continue"),
      ),
    );
  };

  // Step 3: what they're looking for
  const stepPreferences = () => {
    const role = h("input", { type: "text", maxlength: 200, value: draft.targetRole, placeholder: "e.g. Registered Nurse, Software Engineer" });
    const location = h("input", { type: "text", maxlength: 200, value: draft.location, placeholder: "City, town or ZIP (e.g. Ocala, FL or Toronto, ON)" });
    const salary = h("input", { type: "text", inputmode: "numeric", maxlength: 20, value: draft.minimumSalary, placeholder: "Optional, e.g. 70000" });
    const checks = ["remote", "hybrid", "onsite"].map((key) => {
      const input = h("input", { type: "checkbox" });
      input.checked = draft[key];
      return [key, input];
    });
    const error = h("div", { class: "form-error hidden", role: "alert" });

    return h("div", { class: "card stack" },
      h("div", { class: "stack-sm" },
        h("h1", { class: "onboarding-title" }, "What are you looking for?"),
        h("p", { class: "muted" }, "We'll use these as your default search. You can change them anytime."),
      ),
      h("div", { class: "grid-2" },
        h("label", { class: "field" }, "Target role", role),
        h("label", { class: "field" }, "Location", location),
      ),
      h("label", { class: "field" }, "Minimum salary", salary),
      h("div", { class: "stack-sm" },
        h("span", { class: "field-label" }, "Work style"),
        h("div", { class: "row" }, checks.map(([key, input]) =>
          h("label", { class: "check" }, input, { remote: "Remote", hybrid: "Hybrid", onsite: "On-site" }[key]))),
      ),
      error,
      h("div", { class: "row-between" },
        h("button", { class: "btn btn-ghost", onclick: () => go(1) }, "Back"),
        h("button", { class: "btn btn-primary", onclick: async (event) => {
          draft.targetRole = role.value.trim();
          draft.location = location.value.trim();
          draft.minimumSalary = salary.value.trim();
          for (const [key, input] of checks) draft[key] = input.checked;

          const problems = [];
          if (!draft.targetRole) problems.push("Add a target role.");
          if (!draft.location) problems.push("Add a location.");
          if (!draft.remote && !draft.hybrid && !draft.onsite) problems.push("Choose at least one work style.");
          if (problems.length) {
            error.textContent = problems.join(" ");
            error.classList.remove("hidden");
            return;
          }

          event.currentTarget.disabled = true;
          try {
            await saveProfileDraft(draft);
            await savePreferences({ minimumSalary: draft.minimumSalary, remote: draft.remote, hybrid: draft.hybrid, onsite: draft.onsite });
            go(3);
          } catch (err) {
            error.textContent = err.message;
            error.classList.remove("hidden");
            event.currentTarget.disabled = false;
          }
        } }, "Save and continue"),
      ),
    );
  };

  // Step 4: done
  const stepReady = () => {
    const counts = {};
    for (const item of draft.evidence) counts[item.category] = (counts[item.category] || 0) + 1;
    return h("div", { class: "card stack center" },
      h("div", { class: "ready-mark", "aria-hidden": "true" }, "✓"),
      h("h1", { class: "onboarding-title" }, "Profile ready"),
      h("p", { class: "muted" }, `${draft.evidence.length} pieces of evidence: ${counts.experience || 0} experience, ${counts.skill || 0} skills, ${counts.education || 0} education. Searching for ${draft.targetRole} in ${draft.location}.`),
      h("div", { class: "row center" }, h("button", { class: "btn btn-primary", onclick: () => finishOnboarding(true) }, "Find my jobs")),
    );
  };

  draw();
}

async function finishOnboarding(autoSearch) {
  try { await savePreferences({ onboardingComplete: true }); } catch (error) { console.error(error); }
  state.lastSearch = null;
  navigate("/app");
}

// Merge a résumé-derived draft into the editable profile draft.
function mergeDraft(draft, prefill, onlyIfEmpty) {
  if (prefill.summary && (!onlyIfEmpty || !draft.summary)) draft.summary = prefill.summary;
  const existing = new Set(draft.evidence.map((e) => `${e.category}\u0000${e.text.toLowerCase()}`));
  for (const item of prefill.evidence || []) {
    const key = `${item.category}\u0000${item.text.toLowerCase()}`;
    if (!existing.has(key)) {
      draft.evidence.push({ category: item.category, text: item.text });
      existing.add(key);
    }
  }
}

async function saveProfileDraft(draft) {
  const evidence = [];
  for (const [category] of EVIDENCE_SECTIONS) {
    let index = 0;
    const seen = new Set();
    for (const item of draft.evidence.filter((e) => e.category === category)) {
      const text = item.text.trim();
      if (!text || seen.has(text)) continue;
      seen.add(text);
      evidence.push({ category, entryIndex: index++, text, source: `career_profile.${category}` });
    }
  }

  state.profile = await api("/api/v1/career-profile", {
    method: "PUT",
    body: {
      headline: (draft.headline || "").trim(),
      summary: (draft.summary || "").trim(),
      targetRole: (draft.targetRole || "").trim(),
      location: (draft.location || "").trim(),
      evidence,
    },
  });
  return state.profile;
}

// ---------------------------------------------------------------------------
// App shell
// ---------------------------------------------------------------------------

async function appPage(active, view) {
  if (state.user === undefined) await loadUser();
  if (!state.user) {
    navigate("/login", { replace: true });
    return;
  }

  const content = h("div", {}, spinnerRow("Loading…"));
  const usageSlot = h("div", {});
  mount(appShell(active, content, usageSlot));

  await Promise.all([loadBilling(), loadPlans(), loadPreferences().catch(() => null)]);
  if (isInterviewOnly()) mount(appShell(active, content, usageSlot));
  renderUsage(usageSlot);

  if (needsActivation() && active !== "billing") {
    navigate("/app/activate", { replace: true });
    return;
  }


  try {
    const node = await view();
    if (node) content.replaceChildren(node);
  } catch (error) {
    if (error.status === 401) return;
    content.replaceChildren(h("div", { class: "form-error" }, error.message));
  }
}

// Pages an Interview Prep subscriber can use.
const INTERVIEW_PLAN_PAGES = ["home", "interview", "documents", "resumes", "billing"];

function appShell(active, content, usageSlot) {
  const items = [
    ["home", "/app", "⌂", "Dashboard"],
    ["interview", "/app/interview", { img: "/icon-mic.svg" }, "Mock interviews"],
    ["documents", "/app/documents", "✎", "Résumé & cover letter"],
    ["resumes", "/app/resumes", "⇪", "My résumés"],
    ["billing", "/app/billing", "$", "Plan & billing"],
  ];

  return h("div", { class: "shell" },
    h("aside", { class: "sidebar" },
      brand(),
      h("nav", { "aria-label": "Account" },
        items.map(([key, href, icon, label]) =>
          link(href, { class: key === active ? "nav-link active" : "nav-link", "aria-current": key === active ? "page" : undefined },
            h("span", { class: "nav-icon", "aria-hidden": "true" },
              typeof icon === "string" ? icon : h("img", { src: icon.img, alt: "", width: 15, height: 15 })), label)),
      ),
      h("div", { class: "sidebar-foot" },
        usageSlot,
        h("button", { class: "btn btn-ghost btn-sm", onclick: logout }, "Sign out"),
        legalLinks("sidebar-legal"),
      ),
    ),
    h("main", { class: "main" }, content),
  );
}

function planStatusLine(billing) {
  if (!billing) return "";
  switch (billing.source) {
    case "trial": return billing.currentPeriodEnd ? `Free trial · ends ${formatDate(billing.currentPeriodEnd)}` : "Free trial";
    case "subscription": {
      const name = billing.plan === "interview" ? "Prep" : "Pro";
      return billing.cancelAtPeriodEnd && billing.currentPeriodEnd ? `${name} · ends ${formatDate(billing.currentPeriodEnd)}` : name;
    }
    case "pass": return isLifetime(billing) ? "Lifetime Pro" : `Sprint · until ${formatDate(billing.passExpiresAt)}`;
    case "organization": return `Pro via ${billing.organizationName}`;
    case "open": return "Early access";
    default: return state.billing && state.billing.billingEnabled ? "Free plan" : "No plan yet";
  }
}

function renderUsage(slot) {
  const billing = state.billing;
  if (!billing) { slot.replaceChildren(); return; }

  const pro = billing.plan === "pro";
  slot.replaceChildren(h("div", { class: "usage-card" },
    h("div", { class: "row-between" },
      h("span", { class: "small muted" }, planStatusLine(billing)),
      pro && billing.source !== "open" ? h("span", { class: "pill pill-pro" }, billing.source === "trial" ? "TRIAL" : "PRO") : null,
    ),
    pro
      ? null
      : billing.plan === "interview"
        ? link("/app/billing", { class: "btn btn-sm" }, "Upgrade to Pro")
        : link("/app/billing", { class: "btn btn-primary btn-sm" }, "Upgrade to Pro"),
  ));
}

function pageHead(title, subtitle, ...actions) {
  return h("div", { class: "page-head row-between" },
    h("div", {}, h("h1", {}, title), subtitle ? h("p", {}, subtitle) : null),
    actions.length ? h("div", { class: "row" }, actions) : null,
  );
}

function upsell(title, body) {
  return h("div", { class: "card upsell" },
    h("div", {}, h("h3", {}, title), h("p", { class: "muted" }, body)),
    link("/app/billing", { class: "btn btn-primary" }, "See plans"),
  );
}

// ---------------------------------------------------------------------------
// Career profile
// ---------------------------------------------------------------------------

const EVIDENCE_SECTIONS = [
  ["experience", "Experience", "One role or accomplishment per entry. Include years where relevant."],
  ["skill", "Skills", "One skill per entry."],
  ["education", "Education", "Degrees and programs, e.g. “B.S. Computer Science, University of Florida”."],
  ["certification", "Certifications", ""],
  ["license", "Licenses", "Include the license type and state, e.g. “RN, Florida”."],
  ["project", "Projects", ""],
];

// profileEditor renders an editable profile. read() returns the edited values.
function profileEditor(initial, { showRoleFields = false } = {}) {
  const headline = h("input", { type: "text", maxlength: 200, value: initial.headline || "", placeholder: "e.g. Backend engineer focused on distributed systems" });
  const summary = h("textarea", { maxlength: 5000, placeholder: "A short summary of your background." });
  summary.value = initial.summary || "";
  const targetRole = h("input", { type: "text", maxlength: 200, value: initial.targetRole || "", placeholder: "e.g. Software Engineer" });
  const location = h("input", { type: "text", maxlength: 200, value: initial.location || "", placeholder: "City, town or ZIP (e.g. Ocala, FL or Toronto, ON)" });

  const lists = {};
  const sections = EVIDENCE_SECTIONS.map(([category, label, hint]) => {
    const list = h("div", { class: "evidence-list" });
    lists[category] = list;

    const addRow = (text) => {
      const field = h("textarea", { maxlength: 5000, rows: 1, "aria-label": label });
      field.value = text || "";
      const row = h("div", { class: "evidence-item" },
        field,
        h("button", { class: "btn btn-ghost btn-sm", type: "button", "aria-label": `Remove ${label} entry`, onclick: () => row.remove() }, "Remove"),
      );
      list.append(row);
      return field;
    };

    (initial.evidence || [])
      .filter((item) => item.category === category)
      .sort((a, b) => (a.entryIndex || 0) - (b.entryIndex || 0))
      .forEach((item) => addRow(item.text));

    return h("div", { class: "card stack-sm" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, label), hint ? h("p", { class: "faint small" }, hint) : null),
        h("button", { class: "btn btn-sm", type: "button", onclick: () => addRow("").focus() }, "+ Add"),
      ),
      list,
    );
  });

  const node = h("div", { class: "stack" },
    h("div", { class: "card stack" },
      h("label", { class: "field" }, "Headline", headline),
      h("label", { class: "field" }, "Professional summary", summary),
      showRoleFields ? h("div", { class: "grid-2" },
        h("label", { class: "field" }, "Target role", targetRole),
        h("label", { class: "field" }, "Location", location),
      ) : null,
    ),
    sections,
  );

  const read = () => {
    const evidence = [];
    for (const [category] of EVIDENCE_SECTIONS) {
      for (const field of lists[category].querySelectorAll("textarea")) {
        const text = field.value.trim();
        if (text) evidence.push({ category, text });
      }
    }
    const values = { headline: headline.value.trim(), summary: summary.value.trim(), evidence };
    if (showRoleFields) {
      values.targetRole = targetRole.value.trim();
      values.location = location.value.trim();
    }
    return values;
  };

  return { node, read };
}

async function viewProfile() {
  const [profile, resumes] = await Promise.all([loadProfile(true), loadResumes(true)]);
  const container = h("div", {});

  const draw = (values) => {
    const editor = profileEditor(values, { showRoleFields: true });
    const save = h("button", { class: "btn btn-primary", onclick: async () => {
      save.disabled = true;
      try {
        await saveProfileDraft(editor.read());
        state.lastSearch = null;
        toast("Profile saved");
      } catch (error) {
        toast(error.message, true);
      } finally {
        save.disabled = false;
      }
    } }, "Save profile");

    const prefill = resumes.length
      ? h("div", { class: "card row-between" },
          h("div", {}, h("h3", {}, "Prefill from a résumé"), h("p", { class: "muted small" }, "Adds anything new from a résumé. Nothing is removed, and nothing is saved until you click Save.")),
          (() => {
            const select = h("select", { "aria-label": "Résumé to prefill from" }, resumes.map((r) => h("option", { value: r.id }, r.filename)));
            const primary = resumes.find((r) => r.isPrimary);
            if (primary) select.value = primary.id;
            const button = h("button", { class: "btn btn-sm", onclick: async () => {
              button.disabled = true;
              try {
                const current = { ...values, ...editor.read() };
                const prefillData = await api(`/api/v1/resumes/${encodeURIComponent(select.value)}/profile-draft`);
                mergeDraft(current, prefillData, true);
                draw(current);
                toast("Added evidence from your résumé. Review it, then save.");
              } catch (error) {
                button.disabled = false;
                toast(error.message, true);
              }
            } }, "Prefill");
            return h("div", { class: "row" }, select, button);
          })(),
        )
      : h("div", { class: "card row-between" },
          h("p", { class: "muted" }, "Have a résumé? Upload it and we'll fill this in for you."),
          link("/app/resumes", { class: "btn btn-sm" }, "Upload résumé"));

    container.replaceChildren(h("div", { class: "stack" }, prefill, editor.node, h("div", { class: "row" }, save)));
  };

  draw(profile);

  return h("div", {},
    pageHead("Career profile", "Jane matches every job against this profile. Add your wins, tools and credentials to sharpen your matches."),
    container,
  );
}

function profileToSearchProfile(profile) {
  const byCategory = (category) => (profile.evidence || [])
    .filter((item) => item.category === category)
    .sort((a, b) => a.entryIndex - b.entryIndex)
    .map((item) => item.text);

  return {
    about: [profile.headline, profile.summary].filter(Boolean).join("\n"),
    experience: byCategory("experience"),
    education: byCategory("education"),
    skills: byCategory("skill"),
    projects: byCategory("project"),
    certifications: byCategory("certification"),
    licenses: byCategory("license"),
  };
}

// ---------------------------------------------------------------------------
// Résumé library
// ---------------------------------------------------------------------------

async function viewResumes() {
  await loadResumes(true);
  const list = h("div", { class: "stack-sm" });

  const draw = () => {
    if (!state.resumes.length) {
      list.replaceChildren(h("div", { class: "card empty" }, h("h3", {}, "No saved résumés yet"), h("p", {}, "Upload a PDF above to get started.")));
      return;
    }
    list.replaceChildren(...state.resumes.map((resume) => {
      const actions = h("div", { class: "row" },
        resume.isPrimary
          ? h("span", { class: "pill" }, "Default")
          : h("button", { class: "btn btn-sm", onclick: async () => {
              try {
                await api(`/api/v1/resumes/${encodeURIComponent(resume.id)}/primary`, { method: "POST" });
                await loadResumes(true);
                draw();
              } catch (error) { toast(error.message, true); }
            } }, "Make default"),
        h("button", { class: "btn btn-sm", onclick: () => previewResume(resume) }, "View text"),
        h("button", { class: "btn btn-ghost btn-sm", onclick: async (event) => {
          const button = event.currentTarget;
          if (button.dataset.confirm !== "1") {
            button.dataset.confirm = "1";
            button.textContent = "Click again to delete";
            return;
          }
          try {
            await api(`/api/v1/resumes/${encodeURIComponent(resume.id)}`, { method: "DELETE" });
            state.resumeText.delete(resume.id);
            await loadResumes(true);
            draw();
            toast("Résumé deleted");
          } catch (error) { toast(error.message, true); }
        } }, "Delete"),
      );
      return h("div", { class: "card resume-card" },
        h("div", {}, h("div", { class: "resume-name" }, "📄 ", resume.filename), h("div", { class: "faint small" }, `Added ${formatDate(resume.createdAt)}`)),
        actions,
      );
    }));
  };

  draw();

  return h("div", { class: "stack" },
    pageHead("Résumés", "Keep a version for each kind of role. Your default résumé is used for searches, and Pro recommends the best one for every job."),
    h("div", { class: "card" }, resumeDropzone(() => { draw(); toast("Résumé added"); }, { label: "Upload another résumé", hint: "PDF, up to 10 MB. Only the text is kept." })),
    list,
  );
}

async function previewResume(resume) {
  try {
    const text = await resumeText(resume.id);
    openDialog(resume.filename, h("pre", { class: "resume-text" }, text || "(No text)"));
  } catch (error) {
    toast(error.message, true);
  }
}

// ---------------------------------------------------------------------------
// Dialog
// ---------------------------------------------------------------------------

function openDialog(title, ...content) {
  const dialog = h("dialog", { class: "dialog", "aria-label": title },
    h("div", { class: "dialog-head" },
      h("h2", {}, title),
      h("button", { class: "btn btn-ghost btn-sm", "aria-label": "Close", onclick: () => dialog.close() }, "✕"),
    ),
    h("div", { class: "dialog-body stack" }, content),
  );
  dialog.addEventListener("close", () => dialog.remove());
  dialog.addEventListener("click", (event) => { if (event.target === dialog) dialog.close(); });
  document.body.append(dialog);
  dialog.showModal();
  return dialog;
}

// ---------------------------------------------------------------------------
// Job matches
// ---------------------------------------------------------------------------

async function viewJobs() {
  const [profile, preferences, resumes] = await Promise.all([loadProfile(), loadPreferences(), loadResumes()]);
  const hasProfile = (profile.evidence || []).length > 0 || (profile.summary || "").trim() !== "";

  if (!hasProfile && !resumes.length) {
    return h("div", {},
      pageHead("Find jobs"),
      h("div", { class: "card empty" },
        h("h3", {}, "Start with your résumé"),
        h("p", {}, "Upload a résumé or build your profile so we can check each job's requirements against your experience."),
        h("div", { class: "row center" },
          link("/app/resumes", { class: "btn btn-primary" }, "Upload résumé"),
          link("/app/profile", { class: "btn" }, "Build profile"),
        ),
      ),
    );
  }

  const last = state.lastSearch && state.lastSearch.request;
  const targetRole = h("input", { type: "text", required: true, maxlength: 200, placeholder: "e.g. Registered Nurse", value: (last && last.targetRole) || profile.targetRole || "" });
  const location = h("input", { type: "text", required: true, maxlength: 200, placeholder: "City, town or ZIP (e.g. Ocala, FL or Toronto, ON)", value: (last && last.location) || profile.location || "" });
  const minimumSalary = h("input", { type: "text", inputmode: "numeric", placeholder: "Optional", value: (last && last.minimumSalary) || preferences.minimumSalary || "" });
  const remote = h("input", { type: "checkbox" });
  const hybrid = h("input", { type: "checkbox" });
  const onsite = h("input", { type: "checkbox" });
  const source = last || preferences;
  remote.checked = !!source.remote; hybrid.checked = !!source.hybrid; onsite.checked = !!source.onsite;
  // No work style chosen yet means "any": start with all three on.
  if (!remote.checked && !hybrid.checked && !onsite.checked) remote.checked = hybrid.checked = onsite.checked = true;

  // Which evidence to search with: the career profile plus an optional résumé.
  const resumeSelect = h("select", { "aria-label": "Résumé to search with" },
    hasProfile ? h("option", { value: "" }, "Career profile only") : null,
    resumes.map((r) => h("option", { value: r.id }, `${r.filename}${r.isPrimary ? " (default)" : ""}`)),
  );
  const defaultResume = resumes.find((r) => r.isPrimary);
  resumeSelect.value = state.lastSearch ? state.lastSearch.resumeId || "" : defaultResume ? defaultResume.id : "";
  if (!hasProfile && !resumeSelect.value && resumes.length) resumeSelect.value = resumes[0].id;

  const results = h("div", { class: "stack" });
  const submit = h("button", { class: "btn btn-primary", type: "submit" }, "Find my matches");

  const runSearch = async () => {
    const resumeId = resumeSelect.value;
    const request = {
      careerProfile: profileToSearchProfile(profile),
      targetRole: targetRole.value.trim(),
      location: location.value.trim(),
      minimumSalary: minimumSalary.value.trim(),
      remote: remote.checked,
      hybrid: hybrid.checked,
      onsite: onsite.checked,
    };
    if (!request.targetRole || !request.location) {
      toast("Add the job title you want and a city.", true);
      (request.targetRole ? location : targetRole).focus();
      return;
    }
    if (!request.remote && !request.hybrid && !request.onsite) {
      remote.checked = hybrid.checked = onsite.checked = true;
      request.remote = request.hybrid = request.onsite = true;
    }

    submit.disabled = true;
    const progress = h("div", { class: "card" }, spinnerRow("Jane is searching employers…"));
    results.replaceChildren(progress);
    const steps = ["Jane is searching employers…", "Checking every requirement against your résumé…", "Ranking jobs by how well you fit…", "Almost done…"];
    let stepIndex = 0;
    const ticker = setInterval(() => {
      stepIndex = Math.min(stepIndex + 1, steps.length - 1);
      progress.replaceChildren(spinnerRow(steps[stepIndex]));
    }, 6000);

    try {
      if (resumeId) request.resumeText = await resumeText(resumeId);
      const page = await fetchMatchPage(request, 0);
      const { careerProfile, resumeText: _omit, ...shown } = request;
      state.lastSearch = { request: shown, fullRequest: request, matches: page.matches, total: page.total, nextOffset: page.nextOffset, locked: page.locked, resumeId };
      renderMatches(results, state.lastSearch.matches);
      savePreferences({ minimumSalary: request.minimumSalary, remote: request.remote, hybrid: request.hybrid, onsite: request.onsite }).catch(() => {});
    } catch (error) {
      results.replaceChildren(error.code === "pro_required" || error.code === "free_limit_reached"
        ? paywall(error.code === "free_limit_reached" ? "You've used today's free searches" : "Your matches are ready to check", error.code === "free_limit_reached" ? error.message : "Choose a plan to see every job ranked by how well your experience fits, with what you meet, what's partial, and what's missing.")
        : h("div", { class: "form-error" }, error.message));
    } finally {
      clearInterval(ticker);
      submit.disabled = false;
      await loadBilling();
      refreshUsage();
    }
  };

  const form = h("form", { class: "card stack", onsubmit: (event) => { event.preventDefault(); runSearch(); } },
    h("div", { class: "grid-3" },
      h("label", { class: "field" }, "Job title you want", targetRole),
      h("label", { class: "field" }, "City", location),
      h("label", { class: "field" }, "Minimum yearly pay", minimumSalary),
    ),
    h("div", { class: "row-between" },
      h("div", { class: "row" },
        h("label", { class: "check" }, remote, "Remote"),
        h("label", { class: "check" }, hybrid, "Hybrid"),
        h("label", { class: "check" }, onsite, "On-site"),
      ),
      h("div", { class: "row" },
        resumes.length ? h("label", { class: "field inline-field" }, "Search with", resumeSelect) : null,
        submit,
      ),
    ),
  );

  if (state.lastSearch) renderMatches(results, state.lastSearch.matches);
  else results.replaceChildren(h("div", { class: "card empty how-it-works" },
    h("h3", {}, "How it works"),
    h("ol", { class: "steps-inline" },
      h("li", {}, "Type the job title you want and a city."),
      h("li", {}, "Jane searches employers and checks every requirement against your résumé."),
      h("li", {}, "You get jobs ranked best fit first. Open one to see your gaps, build your Application Kit, or save it to My applications.")),
  ));

  const freeNotice = isFree()
    ? h("p", { class: "faint small" }, "Free plan: your top 5 matches per search, up to 10 searches a day. ", link("/pricing", {}, "Pro shows every match"), ".")
    : null;

  if (sessionStorage.getItem("janecareers.autoSearch")) {
    sessionStorage.removeItem("janecareers.autoSearch");
    setTimeout(runSearch, 0);
  }

  return h("div", { class: "stack" },
    pageHead("Find jobs", "Jane finds open jobs and ranks them by how well you fit."),
    h("p", { class: "faint small" }, "Have one specific job in mind? ", link("/app/fit-check", {}, "Check it here"), "."),
    form,
    freeNotice,
    results,
  );
}

// Jobs from Jooble come with only a short preview of the posting,
// so Jane lists them without a score (see previewMatchLevel on the server).
function isPreview(match) {
  return match.matchLevel === "Preview";
}

// sourceCredit credits the aggregator whose jobs are shown.
function sourceCredit(matches) {
  const sources = new Set((matches || []).map((m) => m.source));
  const credits = [
    sources.has("jooble") ? ["Jooble", "https://jooble.org"] : null,
  ].filter(Boolean);
  if (!credits.length) return null;
  const parts = ["Some jobs by "];
  credits.forEach(([name, href], index) => {
    if (index) parts.push(" and ");
    parts.push(h("a", { href, target: "_blank", rel: "noopener noreferrer" }, name));
  });
  return h("p", { class: "faint small" }, ...parts, ".");
}

function fitClass(match) {
  if (isPreview(match)) return "preview";
  const level = (match.matchLevel || "").toLowerCase();
  if (level.includes("best") || level.includes("strong")) return "good";
  if (level.includes("good") || level.includes("moderate")) return "mid";
  return "low";
}

function renderMatches(container, matches) {
  if (!matches.length) {
    container.replaceChildren(h("div", { class: "card empty" },
      h("h3", {}, "No matching jobs were found"),
      h("p", {}, "Try a broader role title, a nearby city, or turn on remote roles."),
    ));
    return;
  }

  const search = state.lastSearch && state.lastSearch.matches === matches ? state.lastSearch : null;
  const total = search && search.total ? search.total : matches.length;
  const list = h("div", { class: "stack" }, ...matches.map(jobCard));
  const locked = search && search.locked ? search.locked : 0;
  const status = h("p", { class: "muted" }, locked
    ? `Your top ${total} of ${total + locked} matches, best fit first`
    : `${total} role${total === 1 ? "" : "s"}, best fit first`);
  const more = h("div", { class: "row center load-more" });
  const credit = h("div", {}, sourceCredit(matches));

  const showButton = () => {
    if (!search || !search.fullRequest || search.nextOffset >= search.total) {
      more.replaceChildren(matches.length > MATCH_PAGE_SIZE ? h("p", { class: "faint small" }, `That's all ${total} jobs Jane found and scored for this search.`) : "");
      return;
    }
    const left = search.total - search.nextOffset;
    const button = h("button", { class: "btn btn-lg", type: "button" }, `Load more jobs (${left} more)`);
    button.onclick = async () => {
      button.disabled = true;
      button.textContent = "Loading…";
      try {
        const page = await fetchMatchPage(search.fullRequest, search.nextOffset);
        search.matches.push(...page.matches);
        search.total = page.total;
        search.nextOffset = page.nextOffset;
        page.matches.forEach((match) => list.append(jobCard(match)));
        credit.replaceChildren(sourceCredit(search.matches) || "");
        showButton();
      } catch (error) {
        button.disabled = false;
        button.textContent = "Load more jobs";
        toast(error.message, true);
      }
    };
    more.replaceChildren(button);
  };
  showButton();

  container.replaceChildren(...[status, list, more, locked ? lockedMatchesCard(locked) : null, credit].filter(Boolean));
}

// lockedMatchesCard: the Free plan shows the best few jobs; Pro shows all.
function lockedMatchesCard(locked) {
  return freeUpgrade(`${locked} more ${locked === 1 ? "match is" : "matches are"} ready for you`,
    "The Free plan shows your top 5 jobs per search. Pro shows every job Jane scored, every fix for your résumé, and a cover letter for each one.");
}

function salaryText(match) {
  if (!match.salaryMin && !match.salaryMax) return null;
  const fmt = (n) => Number(n).toLocaleString(undefined, { maximumFractionDigits: 0 });
  const currency = match.salaryCurrency || "";
  const range = match.salaryMin && match.salaryMax ? `${fmt(match.salaryMin)}–${fmt(match.salaryMax)}` : fmt(match.salaryMin || match.salaryMax);
  return `${currency} ${range}${match.salaryPeriod ? ` / ${match.salaryPeriod}` : ""}`.trim();
}

function safeUrl(value) {
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : null;
  } catch {
    return null;
  }
}

function reqList(kind, title, items) {
  if (!items || !items.length) return null;
  return h("div", { class: "stack-sm" },
    h("span", { class: "section-label" }, `${title} (${items.length})`),
    h("ul", { class: `reqs ${kind}` }, items.map((text) => h("li", {}, text))),
  );
}

async function trackMatch(match) {
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  await api("/api/v1/applications/track", {
    method: "POST",
    body: {
      job: {
        id: match.id || "",
        source: match.source || "",
        title: match.title,
        company: match.company,
        location: match.location || "",
        description: (match.description || "").slice(0, 50000),
        applyURL: applyURL || "",
      },
    },
  });
}

function trackButton(match, extraClass) {
  const button = h("button", { class: `btn btn-sm ${extraClass || ""}`.trim(), onclick: async () => {
    button.disabled = true;
    try {
      await trackMatch(match);
      button.textContent = "Saved ✓";
      toast("Added to My jobs");
    } catch (error) {
      button.disabled = false;
      toast(error.message, true);
    }
  } }, "Save");
  return button;
}

// "12 mi away" when Jane knows how far the job is from the searched place.
function distanceText(match) {
  const miles = match && typeof match.distanceMiles === "number" ? match.distanceMiles : null;
  if (miles === null) return "";
  return miles < 1 ? "In your area" : `${Math.round(miles)} mi away`;
}

function jobCard(match) {
  const cls = fitClass(match);
  const preview = isPreview(match);
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  const meta = [match.location, distanceText(match), match.workArrangement, match.employmentType, salaryText(match)].filter(Boolean);

  return h("article", { class: "card job-card" },
    h("div", { class: "job-head" },
      h("div", { class: "stack-sm" },
        h("div", {},
          h("button", { class: "job-title link-button", onclick: () => openJobDetail(match) }, match.title),
          h("div", { class: "muted" }, match.company),
        ),
        meta.length ? h("div", { class: "row" }, meta.map((m) => h("span", { class: "pill" }, m))) : null,
      ),
      h("div", { class: `score ${cls}`, title: preview ? "Preview only" : match.matchLevel }, preview ? "—" : `${match.matchPercentage}%`),
    ),
    h("div", { class: "row" },
      h("span", { class: `pill pill-${cls}` }, preview ? "Preview only" : (match.matchLevel || "Fit")),
      preview ? null : h("span", { class: "faint small" }, isFree()
        ? `${(match.supportedRequirements || []).length} met · top ${(match.missingRequirements || []).length} missing`
        : `${(match.supportedRequirements || []).length} met · ${(match.partialRequirements || []).length} partial · ${(match.missingRequirements || []).length} missing`),
    ),
    match.explanation ? h("p", { class: "muted" }, match.explanation) : null,
    h("div", { class: "row" },
      h("button", { class: "btn btn-sm", onclick: () => openJobDetail(match) }, "Job details"),
      applyURL ? h("a", { class: "btn btn-primary btn-sm", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, preview ? "Open posting ↗" : "Apply ↗") : null,
      preview ? link("/app/fit-check", { class: "btn btn-sm" }, "Check my fit") : null,
      trackButton(match),
    ),
  );
}

// openJobDetail: one job, step by step (Nov 2026). The left side shows the
// match and five steps; the right side shows one step at a time:
// what to change, your résumé, cover letter, apply, practice the interview.
// Preview-only postings (no full description) keep the simple view.
function openJobDetail(match) {
  if (isPreview(match)) return openJobDetailSimple(match);

  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  const company = match.company || "the employer";
  const norm = (text) => (text || "").toLowerCase().replace(/\s+/g, " ").trim();
  const required = new Set((match.requirements || []).filter((r) => r.importance === "required").map((r) => norm(r.text)));
  const supported = match.supportedRequirements || [];
  const mustMet = supported.filter((t) => required.has(norm(t))).length;
  const gaps = gapsFromMatch(match);
  const level = match.matchLevel || "Fit";
  const levelTone = fitClass(match);

  const getInput = async () => {
    const resumes = state.resumes || await loadResumes();
    const chosen = (state.lastSearch && state.lastSearch.resumeId) || ((resumes.find((r) => r.isPrimary) || resumes[0] || {}).id);
    if (!chosen) throw new Error("Upload a résumé first, then Jane can tailor it.");
    return {
      resumeText: await resumeText(chosen),
      jobDescription: (match.description || "").slice(0, 30000),
      jobTitle: match.title,
      company: match.company,
      gaps: gaps.map((g) => g.requirement),
    };
  };

  const steps = [
    { title: "What to change", note: gaps.length ? `${gaps.length} ${gaps.length === 1 ? "thing" : "things"}` : "Nothing to fix", render: stepChange },
    { title: "Your résumé", note: "Tailored for this job", render: stepResume },
    { title: "Cover letter", note: `For ${company}`, render: stepLetter },
    { title: "Apply", note: `On ${company}'s site`, render: stepApply },
    { title: "Practice the interview", note: "Out loud", render: stepPractice },
  ];
  const rendered = new Map();
  let current = 0;

  const navButtons = steps.map((step, index) => h("button", { type: "button", class: "js-step", onclick: () => show(index) },
    h("span", { class: "js-num" }, String(index + 1)),
    h("span", { class: "js-step-text" }, h("b", {}, step.title), h("small", {}, step.note))));
  const panel = h("section", { class: "js-panel", "aria-live": "polite" });

  function show(index) {
    current = index;
    navButtons.forEach((button, i) => {
      button.classList.toggle("on", i === index);
      if (i === index) button.setAttribute("aria-current", "step"); else button.removeAttribute("aria-current");
    });
    // On phones the steps are one scrolling row: keep the current one in view.
    const nav = navButtons[index].parentElement;
    if (nav && nav.scrollWidth > nav.clientWidth) nav.scrollTo({ left: navButtons[index].offsetLeft - 8, behavior: "smooth" });
    if (!rendered.has(index)) rendered.set(index, steps[index].render());
    const back = index > 0 ? h("button", { type: "button", class: "btn btn-ghost", onclick: () => show(index - 1) }, "← Back") : h("span", {});
    const next = index < steps.length - 1
      ? h("button", { type: "button", class: "btn btn-primary", onclick: () => show(index + 1) }, `Next: ${steps[index + 1].title.toLowerCase()} →`)
      : h("button", { type: "button", class: "btn", onclick: () => dialog.close() }, "Done");
    panel.replaceChildren(
      h("p", { class: "js-kicker" }, `Step ${index + 1} of ${steps.length}`),
      h("h2", { class: "js-h2" }, steps[index].title),
      rendered.get(index),
      h("div", { class: "js-foot" }, back, next));
    panel.scrollTop = 0;
  }

  function stepChange() {
    const missingMust = gaps.filter((g) => g.kind !== "strengthen" && g.importance === "required");
    const clearer = gaps.filter((g) => g.kind === "strengthen");
    const niceToHave = gaps.filter((g) => g.kind !== "strengthen" && g.importance !== "required");
    const group = (title, hint, items) => items.length ? h("div", { class: "stack-sm" },
      h("span", { class: "section-label" }, title),
      hint ? h("p", { class: "faint small" }, hint) : null,
      h("ul", { class: "gap-list" }, items.map((gap) => gapItem(gap)))) : null;
    return h("div", { class: "stack" },
      h("p", { class: "muted" }, required.size
        ? `You meet ${mustMet} of ${required.size} must-haves.${gaps.length ? " Tell Jane what you have and your score goes up." : ""}`
        : (match.explanation || "")),
      gaps.length ? null : h("p", {}, "Jane found everything this job asks for on your résumé. Apply early."),
      group("Missing must-haves", "Not on your résumé. If you've done it, add it with one real example. If you haven't, don't.", missingMust),
      group("Say these more clearly", "Your résumé hints at these. One specific example makes them count.", clearer),
      group("Nice to have", "", niceToHave),
      isFree() ? freeUpgrade("See every fix", "The Free plan shows the top 3 things this job asks for that you're missing. Pro shows all of them, plus what to say more clearly.") : null,
      supported.length ? h("div", { class: "stack-sm" },
        h("span", { class: "section-label" }, "Already strong"),
        h("ul", { class: "js-strong" }, supported.slice(0, 8).map((text) => h("li", {}, text)))) : null);
  }

  function stepResume() {
    const slot = h("div", {});
    return h("div", { class: "stack" },
      h("p", { class: "muted" }, "Your résumé, rewritten for this job so the recruiter sees a match in seconds. Jane only uses your real experience."),
      isFree()
        ? freeUpgrade("Get a résumé made for this job", "With Pro, Jane rewrites your résumé to match what this employer is looking for, picks your strongest résumé to send, and gives it to you as a PDF or Word file.")
        : recommendationCard(match, slot),
      isFree() ? null : tailoredResumeSection(getInput),
      isFree() ? null : applicationKitSection(getInput, `${match.title}${match.company ? ` at ${match.company}` : ""}`, { withLetter: false }));
  }

  function stepLetter() {
    return h("div", { class: "stack" },
      h("p", { class: "muted" }, `A short, personal cover letter that tells ${company} why you're a great fit, ready to send as a PDF or Word file.`),
      coverLetterSection(getInput));
  }

  function stepApply() {
    const details = h("details", { class: "js-posting" },
      h("summary", {}, "Read the full posting"),
      h("div", { class: "description" }, match.description || ""));
    return h("div", { class: "stack" },
      h("p", { class: "muted" }, `Apply on ${company}'s own site, not a job board. Your application goes straight to them.`),
      applyURL ? h("a", { class: "btn btn-primary btn-lg", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, `Apply on ${company}'s site ↗`) : null,
      h("div", { class: "row" }, trackButton(match), h("span", { class: "faint small" }, "Save it to keep track of where you applied.")),
      match.description ? details : null);
  }

  function stepPractice() {
    const rounds = [["mixed", "Not sure yet"], ["phone", "Phone screen"], ["first", "First round"], ["role", "Technical"],
      ["design", "System design"], ["behavioral", "Behavioral"], ["panel", "Panel"], ["final", "Final round"]];
    let chosen = "mixed";
    const chips = rounds.map(([value, label]) => {
      const chip = h("button", { type: "button", class: value === chosen ? "round-chip on" : "round-chip", "aria-pressed": value === chosen ? "true" : "false", onclick: () => {
        chosen = value;
        chips.forEach((c) => { const on = c === chip; c.classList.toggle("on", on); c.setAttribute("aria-pressed", on ? "true" : "false"); });
      } }, label);
      return chip;
    });
    return h("div", { class: "stack" },
      h("p", { class: "muted" }, `Walk into your ${company} interview ready. Jane runs a mock interview built for this job: she asks what this interviewer is likely to ask, you answer out loud, and she coaches you after every answer.`),
      h("div", { class: "stack-sm" },
        h("span", { class: "section-label" }, "Which round are you preparing for?"),
        h("div", { class: "round-chips" }, chips)),
      h("ul", { class: "ticks" },
        h("li", {}, "Questions for this company and this round, from phone screens to system design"),
        h("li", {}, "Your pace, filler words and answer length after every answer"),
        h("li", {}, "What you did well, what to fix, and a stronger version of your answer")),
      h("button", { class: "btn btn-primary btn-lg", type: "button", onclick: () => {
        state.practiceJob = { title: match.title, company: match.company, description: (match.description || "").slice(0, 30000), stage: chosen };
        dialog.close();
        startInterviewPrep();
      } }, "Start my mock interview"));
  }

  const dialog = h("dialog", { class: "dialog dialog-job", "aria-label": match.title },
    h("div", { class: "dialog-head" },
      h("div", { class: "js-title" },
        h("h2", {}, match.title),
        h("span", { class: "muted small" }, [match.company, match.location, distanceText(match), salaryText(match)].filter(Boolean).join(" · "))),
      h("div", { class: "js-head-actions" },
        trackButton(match),
        h("button", { class: "btn btn-ghost btn-sm", "aria-label": "Close", onclick: () => dialog.close() }, "✕"))),
    h("div", { class: "js-body" },
      h("nav", { class: "js-nav", "aria-label": "Steps" },
        h("div", { class: "js-score" },
          h("div", { class: `score ${levelTone}` }, `${match.matchPercentage}%`),
          h("div", {}, h("b", { class: `text-${levelTone}` }, level),
            h("small", {}, required.size ? `${mustMet} of ${required.size} must-haves` : `${supported.length} requirements met`))),
        navButtons),
      panel));
  dialog.addEventListener("close", () => dialog.remove());
  dialog.addEventListener("click", (event) => { if (event.target === dialog) dialog.close(); });
  document.body.append(dialog);
  show(0);
  dialog.showModal();
  return dialog;
}

function openJobDetailSimple(match) {
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  const cls = fitClass(match);
  const required = (match.requirements || []).filter((r) => r.importance === "required").map((r) => r.text);
  const preferred = (match.requirements || []).filter((r) => r.importance === "preferred").map((r) => r.text);
  const recommendationSlot = h("div", {});
  const preview = isPreview(match);

  openDialog(match.title,
    h("div", { class: "row-between" },
      h("div", {}, h("div", { class: "muted" }, match.company), h("div", { class: "faint small" }, [match.location, distanceText(match), salaryText(match)].filter(Boolean).join(" · "))),
      h("div", { class: `score ${cls}` }, preview ? "—" : `${match.matchPercentage}%`),
    ),
    h("div", { class: "row" },
      applyURL ? h("a", { class: "btn btn-primary btn-sm", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, preview ? "Open posting ↗" : "Apply ↗") : null,
      preview ? h("button", { class: "btn btn-sm", onclick: (event) => { event.currentTarget.closest("dialog")?.close(); navigate("/app/fit-check"); } }, "Check my fit") : null,
      trackButton(match),
    ),
    h("div", { class: "card inset stack" },
      h("h3", {}, "Your match"),
      match.explanation ? h("p", { class: "muted" }, match.explanation) : null,
      reqList("supported", "You meet", match.supportedRequirements),
    ),
    preview ? null : gapCloser(gapsFromMatch(match)),
    match.description && !preview ? applicationKitSection(async () => {
      const resumes = state.resumes || await loadResumes();
      const chosen = (state.lastSearch && state.lastSearch.resumeId) || ((resumes.find((r) => r.isPrimary) || resumes[0] || {}).id);
      if (!chosen) throw new Error("Upload a résumé first, then Jane can tailor it.");
      return {
        resumeText: await resumeText(chosen),
        jobDescription: (match.description || "").slice(0, 30000),
        jobTitle: match.title,
        company: match.company,
        gaps: gapsFromMatch(match).map((g) => g.requirement),
      };
    }, `${match.title}${match.company ? ` at ${match.company}` : ""}`) : null,
    preview ? null : recommendationCard(match, recommendationSlot),
    !preview && (required.length || preferred.length) ? h("div", { class: "stack-sm" },
      h("h3", {}, "Posting requirements"),
      required.length ? h("div", {}, h("span", { class: "section-label" }, "Required"), h("ul", { class: "plain-list" }, required.map((t) => h("li", {}, t)))) : null,
      preferred.length ? h("div", {}, h("span", { class: "section-label" }, "Preferred"), h("ul", { class: "plain-list" }, preferred.map((t) => h("li", {}, t)))) : null,
    ) : null,
    match.description ? h("div", { class: "stack-sm" }, h("h3", {}, preview ? "Preview of the posting" : "Job description"), h("div", { class: "description" }, match.description)) : null,
  );
}

// "Which résumé should I send?" Pro ranks every saved résumé for this job.
function recommendationCard(match, slot) {
  const resumes = state.resumes || [];
  if (resumes.length < 1) {
    slot.replaceChildren(h("div", { class: "card inset row-between" },
      h("p", { class: "muted small" }, "Upload your résumés and Pro will tell you which one fits this job best."),
      link("/app/resumes", { class: "btn btn-sm" }, "Add résumés")));
    return slot;
  }
  if (!isPro()) {
    slot.replaceChildren(h("div", { class: "card inset row-between" },
      h("div", {}, h("h3", {}, "Recommended résumé"), h("p", { class: "muted small" }, `Pro checks all ${resumes.length} of your résumés against this job and tells you which to send.`)),
      link("/app/billing", { class: "btn btn-primary btn-sm" }, "Upgrade")));
    return slot;
  }

  const button = h("button", { class: "btn btn-sm", onclick: async () => {
    button.disabled = true;
    slot.replaceChildren(h("div", { class: "card inset" }, spinnerRow("Comparing your résumés against this job…")));
    try {
      const inputs = await Promise.all(resumes.map(async (r) => ({ id: r.id, name: r.filename, resumeText: await resumeText(r.id) })));
      const result = await api("/api/v1/resume-recommendation", {
        method: "POST",
        body: {
          job: {
            id: match.id || "",
            title: match.title,
            company: match.company,
            description: match.description || "",
            requirements: match.requirements || [],
            isActive: true,
            applyURL: match.applyURL || "",
          },
          resumes: inputs,
        },
      });
      slot.replaceChildren(h("div", { class: "card inset stack-sm" },
        h("h3", {}, "Recommended résumé"),
        result.results.map((r, index) => h("div", { class: index === 0 ? "rec-row best" : "rec-row" },
          h("div", {}, h("div", { class: "resume-name" }, index === 0 ? "★ " : "", r.name), h("div", { class: "faint small" }, `${r.supportedRequirements.length} met · ${r.partialRequirements.length} partial · ${r.missingRequirements.length} missing`)),
          h("span", { class: `pill pill-${fitClass(r)}` }, `${r.matchPercentage}%`),
        )),
      ));
    } catch (error) {
      slot.replaceChildren(h("div", { class: "card inset" }, h("p", { class: "text-error small" }, error.code === "pro_required" ? "Résumé recommendation needs Pro." : `Résumé recommendation unavailable: ${error.message}`)));
    }
  } }, "Which résumé should I send?");

  slot.replaceChildren(h("div", { class: "card inset row-between" },
    h("div", {}, h("h3", {}, "Recommended résumé"), h("p", { class: "muted small" }, `Compare your ${resumes.length} résumé${resumes.length === 1 ? "" : "s"} against this job.`)),
    button));
  return slot;
}

// ---------------------------------------------------------------------------
// My jobs (application tracker)
// ---------------------------------------------------------------------------

const STATUS_LABELS = {
  saved: "Saved",
  applied: "Applied",
  received: "Received",
  recruiter_contact: "Recruiter contact",
  interviewing: "Interview",
  offer: "Offer",
  hired: "Hired",
  rejected: "Rejected",
  withdrawn: "Withdrawn",
};

const STATUS_EVENT = {
  saved: "status_changed",
  applied: "application_submitted",
  received: "application_received",
  recruiter_contact: "recruiter_contact",
  interviewing: "interview_scheduled",
  offer: "offer_received",
  hired: "hired",
  rejected: "rejected",
  withdrawn: "withdrawn",
};

const COLUMNS = [
  ["Saved", ["saved"]],
  ["Applied", ["applied", "received", "recruiter_contact"]],
  ["Interview", ["interviewing"]],
  ["Offer", ["offer", "hired"]],
  ["Closed", ["rejected", "withdrawn"]],
];

// stageOf maps a status to its column on the tracker.
function stageOf(status) {
  const column = COLUMNS.find(([, statuses]) => statuses.includes(status));
  return column ? column[0] : "Saved";
}

const STAGE_TONE = { Saved: "", Applied: "mid", Interview: "good", Offer: "good", Closed: "low" };

async function viewApplications() {
  const params = new URLSearchParams(location.search);
  if (params.get("gmail") === "connected") {
    history.replaceState(null, "", "/app/applications");
    toast("Gmail connected. Sync to update your applications.");
  }

  const [applications, plansInfo] = await Promise.all([api("/api/v1/applications"), loadPlans()]);
  // "Active" shows everything that isn't closed; a stage tile narrows it.
  let filter = "Active";
  const tiles = h("div", { class: "stage-tiles", role: "tablist", "aria-label": "Filter by stage" });
  const list = h("div", { class: "stack-sm" });

  const inFilter = (application) => {
    const stage = stageOf(application.status);
    return filter === "Active" ? stage !== "Closed" : stage === filter;
  };

  const draw = () => {
    const counts = Object.fromEntries(COLUMNS.map(([title]) => [title, 0]));
    applications.forEach((a) => { counts[stageOf(a.status)] += 1; });
    const active = applications.length - counts.Closed;

    tiles.replaceChildren(...[["Active", active], ...COLUMNS.map(([title]) => [title, counts[title]])].map(([title, count]) =>
      h("button", {
        class: title === filter ? "stage-tile on" : "stage-tile",
        role: "tab",
        "aria-selected": title === filter ? "true" : "false",
        onclick: () => { filter = title; draw(); },
      }, h("b", {}, String(count)), h("span", {}, title))));

    const shown = applications
      .filter(inFilter)
      .sort((a, b) => new Date(b.updatedAt || b.createdAt) - new Date(a.updatedAt || a.createdAt));
    list.replaceChildren(...(shown.length
      ? shown.map((application) => applicationCard(application, (updated) => {
          Object.assign(application, updated);
          draw();
        }))
      : [h("div", { class: "card empty" },
          h("p", { class: "muted" }, filter === "Active" ? "No active applications." : `Nothing in ${filter} yet.`))]));
  };
  draw();

  const reload = async () => {
    const fresh = await api("/api/v1/applications");
    applications.splice(0, applications.length, ...fresh);
    draw();
  };

  const body = applications.length
    ? h("div", { class: "stack" }, tiles, list)
    : h("div", { class: "card empty" },
        h("h3", {}, "Nothing tracked yet"),
        h("p", {}, "Hit “Save” on any job match to add it here."),
        h("div", { class: "row center" }, link("/app/jobs", { class: "btn btn-primary" }, "Find jobs")),
      );

  return h("div", { class: "stack" },
    pageHead("My applications", "Track every job from saved to offer. Each change is saved to the job's timeline.",
      link("/app/jobs", { class: "btn btn-sm" }, "Find jobs")),
    gmailCard(plansInfo, reload),
    body,
  );
}

function gmailCard(plansInfo, onSynced) {
  const card = h("div", { class: "card gmail-card" });

  if (!plansInfo.gmailEnabled) return null;

  if (!isPro()) {
    card.replaceChildren(
      h("div", {}, h("h3", {}, "Gmail sync"), h("p", { class: "muted small" }, "Pro reads recruiter emails, like confirmations, interview invites and rejections, and updates your jobs for you.")),
      link("/app/billing", { class: "btn btn-primary btn-sm" }, "Upgrade"),
    );
    return card;
  }

  const draw = async () => {
    card.replaceChildren(spinnerRow("Checking Gmail…"));
    let status;
    try {
      status = await api("/api/v1/email/gmail/status");
    } catch (error) {
      card.replaceChildren(h("p", { class: "text-error small" }, error.message));
      return;
    }

    if (!status.connected) {
      card.replaceChildren(
        h("div", {}, h("h3", {}, "Connect Gmail"), h("p", { class: "muted small" }, "Read-only access. We only look for job-application emails and update matching jobs on your board.")),
        h("a", { class: "btn btn-primary btn-sm", href: "/api/v1/email/gmail/connect" }, "Connect Gmail"),
      );
      return;
    }

    const sync = h("button", { class: "btn btn-primary btn-sm", onclick: async () => {
      sync.disabled = true;
      sync.textContent = "Syncing…";
      try {
        const result = await api("/api/v1/email/gmail/sync", { method: "POST" });
        toast(`Checked ${result.messages_fetched} emails: ${result.updated} job${result.updated === 1 ? "" : "s"} updated.`);
        await onSynced();
      } catch (error) {
        toast(error.message, true);
      } finally {
        sync.disabled = false;
        sync.textContent = "Sync now";
      }
    } }, "Sync now");

    const disconnect = h("button", { class: "btn btn-ghost btn-sm", onclick: async () => {
      if (disconnect.dataset.confirm !== "1") {
        disconnect.dataset.confirm = "1";
        disconnect.textContent = "Click again to disconnect";
        return;
      }
      try {
        await api("/api/v1/email/gmail/disconnect", { method: "DELETE" });
        toast("Gmail disconnected");
        draw();
      } catch (error) { toast(error.message, true); }
    } }, "Disconnect");

    card.replaceChildren(
      h("div", {}, h("h3", {}, "Gmail connected"), h("p", { class: "muted small" }, status.email_address ? `Watching ${status.email_address} for application updates.` : "Watching for application updates.")),
      h("div", { class: "row" }, sync, disconnect),
    );
  };

  draw();
  return card;
}

function applicationCard(application, onUpdate) {
  const job = application.job || {};
  const select = h("select", { "aria-label": "Status" },
    Object.entries(STATUS_LABELS).map(([value, label]) => h("option", { value }, label)));
  select.value = application.status;

  select.addEventListener("change", async () => {
    const newStatus = select.value;
    select.disabled = true;
    try {
      const result = await api(`/api/v1/applications/${encodeURIComponent(application.id)}/events`, {
        method: "POST",
        body: { eventType: STATUS_EVENT[newStatus], newStatus, summary: `Moved to ${STATUS_LABELS[newStatus]}.` },
      });
      onUpdate(result.application);
      toast(`Moved to ${STATUS_LABELS[newStatus]}`);
    } catch (error) {
      select.value = application.status;
      select.disabled = false;
      toast(error.message, true);
    }
  });

  const timeline = h("ul", { class: "timeline" });
  const details = h("details", { class: "more", ontoggle: async () => {
    if (!details.open || timeline.dataset.loaded) return;
    timeline.dataset.loaded = "1";
    timeline.replaceChildren(h("li", {}, "Loading…"));
    try {
      const events = await api(`/api/v1/applications/${encodeURIComponent(application.id)}/events`);
      timeline.replaceChildren(...(events.length
        ? events.map((e) => h("li", {}, `${formatDate(e.occurredAt)} · ${STATUS_LABELS[e.newStatus] || e.newStatus}${e.source === "email" ? " · from Gmail" : ""}${e.summary ? ` · ${e.summary}` : ""}`))
        : [h("li", {}, `Saved ${formatDate(application.createdAt)}`)]));
    } catch (error) {
      timeline.replaceChildren(h("li", {}, error.message));
    }
  } }, h("summary", { class: "small" }, "Timeline"), timeline);

  const applyURL = safeUrl(job.applyURL || job.sourceURL || "");
  const stage = stageOf(application.status);
  const when = application.appliedAt
    ? `Applied ${formatDate(application.appliedAt)}`
    : `Saved ${formatDate(application.createdAt)}`;

  return h("article", { class: "card app-row" },
    h("div", { class: "app-row-main" },
      h("div", { class: "row app-row-title" },
        h("span", { class: "title" }, job.title || "Untitled job"),
        h("span", { class: `pill pill-${STAGE_TONE[stage] || "plain"}` }, STATUS_LABELS[application.status] || stage)),
      h("div", { class: "muted small" }, [job.company, job.location].filter(Boolean).join(" · ")),
      h("div", { class: "faint small" }, when),
    ),
    h("div", { class: "app-row-side" },
      h("label", { class: "app-status" }, h("span", { class: "faint small" }, "Status"), select),
      h("div", { class: "row app-row-actions" },
        applyURL ? h("a", { class: "btn btn-sm", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, "Open posting ↗") : null,
        application.jobId ? link(`/app/interview?job=${encodeURIComponent(application.jobId)}`, { class: "btn btn-sm" }, "Practice interview") : null),
    ),
    h("div", { class: "app-row-timeline" }, details),
  );
}

// ---------------------------------------------------------------------------
// Billing
// ---------------------------------------------------------------------------

async function viewBilling() {
  const params = new URLSearchParams(location.search);
  const checkout = params.get("checkout");
  const verified = params.get("verified");
  if (checkout || verified) history.replaceState(null, "", "/app/billing");

  if (verified === "1") toast("Email confirmed.");
  if (verified === "invalid") toast("That confirmation link is invalid or expired. Send a new one below.", true);

  if (checkout === "success" || checkout === "school") {
    toast(checkout === "school" ? "School license purchased. Your students can sign up now." : `You're in! Welcome to ${BRAND} Pro.`);
    // The webhook may land a moment after the redirect.
    for (let attempt = 0; attempt < 6; attempt++) {
      await loadBilling();
      if (checkout === "school" ? state.billing.ownedLicense && state.billing.ownedLicense.active : isPro()) break;
      await new Promise((resolve) => setTimeout(resolve, 1500));
    }
    refreshUsage();
  }

  const billing = state.billing || await loadBilling();
  const plansInfo = await loadPlans();
  const pro = isPro();
  const interviewOnly = isInterviewOnly();

  const sections = [pageHead("Plan & billing", billing.email ? `Signed in as ${billing.email}` : null)];

  // Email confirmation
  if (billing.emailEnabled && !billing.emailVerified) {
    const resend = h("button", { class: "btn btn-sm", onclick: async () => {
      resend.disabled = true;
      try {
        await api("/api/v1/auth/verify-email/send", { method: "POST" });
        toast(`Confirmation email sent to ${billing.email}.`);
      } catch (error) {
        toast(error.message, true);
      } finally {
        resend.disabled = false;
      }
    } }, "Send confirmation email");
    sections.push(h("div", { class: "card row-between notice" },
      h("div", {}, h("h3", {}, "Confirm your email"), h("p", { class: "muted small" }, "We sent you a link when you signed up. Confirming lets you reset your password.")),
      resend));
  }

  // Current plan
  const manage = h("button", { class: "btn", onclick: async () => {
    manage.disabled = true;
    try {
      const { url } = await api("/api/v1/billing/portal", { method: "POST" });
      window.location.assign(url);
    } catch (error) {
      manage.disabled = false;
      toast(error.message, true);
    }
  } }, billing.source === "trial" ? "Manage or cancel trial" : "Manage billing");

  const detail = (() => {
    switch (billing.source) {
      case "trial": return billing.currentPeriodEnd
        ? `Your trial ends ${formatDate(billing.currentPeriodEnd)}. ${billing.cancelAtPeriodEnd ? "It won't renew." : `Then ${(planFor("monthly") || {}).displayPrice || ""}/month unless you cancel.`}`
        : "You're on a free trial.";
      case "subscription": {
        if (!billing.currentPeriodEnd) return "Active.";
        if (billing.status === "past_due") return "Payment past due. Update your card to keep Pro.";
        return billing.cancelAtPeriodEnd ? `Cancels on ${formatDate(billing.currentPeriodEnd)}.` : `Renews on ${formatDate(billing.currentPeriodEnd)}.`;
      }
      case "pass": return isLifetime(billing) ? "You have Lifetime Pro. Everything in Pro, with no monthly bill." : `Your Sprint gives you Pro until ${formatDate(billing.passExpiresAt)}. It doesn't renew.`;
      case "organization": return `You have Pro through ${billing.organizationName}'s license.`;
      case "open": return "Full access during early access.";
      default: return billing.billingEnabled
        ? "Your top 5 matches per search, the top 3 things you're missing, 1 cover letter and 1 practice question. Upgrade for everything."
        : "Choose a plan to start matching.";
    }
  })();

  sections.push(h("div", { class: "card stack" },
    h("div", { class: "plan-row" },
      h("div", {},
        h("div", { class: "row" }, h("span", { class: "plan-name" }, pro || interviewOnly ? planStatusLine(billing) : (billing.billingEnabled ? "Free plan" : "No plan yet")), pro && billing.source !== "open" ? h("span", { class: "pill pill-pro" }, billing.source === "trial" ? "TRIAL" : "PRO") : interviewOnly ? h("span", { class: "pill pill-pro" }, "PREP") : null),
        h("p", { class: "muted" }, detail),
      ),
      billing.canManageBilling ? manage : null,
    ),
  ));

  // Interview Prep subscribers upgrade by switching plans in Stripe.
  if (interviewOnly) {
    sections.push(upgradeFromInterview("Want unlimited practice?", "Your Prep plan includes 40 graded answers a month."));
  }

  // Personal plans
  if (!interviewOnly && (!pro || billing.source === "pass")) {
    sections.push(h("div", { class: "stack" },
      h("h2", {}, pro ? "Extend your access" : "Upgrade your plan"),
      plansInfo.billingEnabled
        ? h("div", { class: "grid-2" }, planCards(plansInfo, { compact: true }))
        : h("div", { class: "card muted" }, "Plans are coming soon."),
      plansInfo.billingEnabled ? checkoutNote() : null,
    ));
  }


  // Danger zone
  const deleteButton = h("button", { class: "btn btn-danger btn-sm", onclick: async () => {
    if (deleteButton.dataset.confirm !== "1") {
      deleteButton.dataset.confirm = "1";
      deleteButton.textContent = "Click again to permanently delete";
      return;
    }
    deleteButton.disabled = true;
    try {
      await api("/api/v1/account", { method: "DELETE" });
      resetState();
      toast("Your account has been deleted");
      navigate("/");
    } catch (error) {
      deleteButton.disabled = false;
      toast(error.message, true);
    }
  } }, "Delete my account");

  sections.push(h("div", { class: "card stack-sm" },
    h("h3", {}, "Delete account"),
    h("p", { class: "muted small" }, "Permanently deletes your profile, résumés and tracked jobs, and cancels any personal subscription immediately."),
    h("div", { class: "row" }, deleteButton),
  ));

  return h("div", { class: "stack" }, sections);
}

const PUBLIC_EMAIL_DOMAINS = new Set(["gmail.com", "googlemail.com", "yahoo.com", "ymail.com", "outlook.com", "hotmail.com", "live.com", "msn.com", "icloud.com", "me.com", "mac.com", "aol.com", "proton.me", "protonmail.com", "pm.me", "gmx.com", "gmx.net", "mail.com", "yandex.com", "zoho.com", "hey.com", "fastmail.com", "tutanota.com", "qq.com", "163.com", "comcast.net", "att.net", "verizon.net"]);

function schoolLicenseCard(billing, plansInfo) {
  const school = planFor("school");
  const owned = billing.ownedLicense;
  const domain = (billing.email || "").split("@")[1] || "";

  if (owned && owned.active) {
    const manage = h("button", { class: "btn btn-sm", onclick: async () => {
      manage.disabled = true;
      try {
        const { url } = await api("/api/v1/billing/portal", { method: "POST", body: { target: "organization" } });
        window.location.assign(url);
      } catch (error) {
        manage.disabled = false;
        toast(error.message, true);
      }
    } }, "Manage license billing");
    return h("div", { class: "card stack-sm" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, `${owned.name} license`), h("p", { class: "muted small" },
          `Everyone who signs up and confirms an @${owned.domain} email gets Pro.` +
          (owned.currentPeriodEnd ? ` ${owned.cancelAtPeriodEnd ? "Ends" : "Renews"} ${formatDate(owned.currentPeriodEnd)}.` : ""))),
        owned.canManage ? manage : null,
      ),
      h("p", { class: "faint small" }, `Share this with students: “Sign up at ${location.origin}/signup with your @${owned.domain} email, then confirm it.”`),
    );
  }

  const base = h("div", { class: "stack-sm" },
    h("h3", {}, "School & program license"),
    h("p", { class: "muted small" }, `${school ? school.displayPrice : "$1,000"} per year. Every student and staff member who confirms an email at your school's domain gets Pro. Only that exact domain is covered, not subdomains.`),
  );

  if (!plansInfo.billingEnabled || !school) {
    return h("div", { class: "card stack-sm" }, base, h("p", { class: "faint small" }, "Email us from the Contact page to set up a license or a free pilot."));
  }
  if (!billing.emailEnabled) {
    return h("div", { class: "card stack-sm" }, base, h("p", { class: "faint small" }, "School licenses open soon. Email us from the Contact page to reserve one or start a pilot."));
  }
  if (PUBLIC_EMAIL_DOMAINS.has(domain)) {
    return h("div", { class: "card stack-sm" }, base, h("p", { class: "faint small" }, `You're signed in with a personal @${domain} address. To buy a license, sign up with your school email instead.`));
  }
  if (!billing.emailVerified) {
    return h("div", { class: "card stack-sm" }, base, h("p", { class: "faint small" }, `Confirm your email above first. The license will cover @${domain}.`));
  }

  const name = h("input", { type: "text", maxlength: 120, placeholder: "e.g. University of Florida College of Nursing", "aria-label": "School or program name" });
  const buy = h("button", { class: "btn btn-primary", onclick: async () => {
    if (name.value.trim().length < 2) {
      toast("Enter your school or program name", true);
      name.focus();
      return;
    }
    buy.disabled = true;
    await startCheckout("school", { organizationName: name.value.trim() });
    buy.disabled = false;
  } }, `Buy license for @${domain}`);

  return h("div", { class: "card stack-sm" },
    base,
    h("label", { class: "field" }, "School or program name", name),
    h("div", { class: "row" }, buy),
    h("p", { class: "legal-note" }, "Renews yearly until canceled. Need an invoice or purchase order instead? Email us from the ", h("a", { href: "/contact" }, "Contact"), " page."),
  );
}

// ---------------------------------------------------------------------------
// Forgot / reset password
// ---------------------------------------------------------------------------

async function viewForgotPassword() {
  const error = h("div", { class: "form-error hidden", role: "alert" });
  const done = h("div", { class: "card notice hidden", role: "status" });
  const email = h("input", { type: "email", name: "email", autocomplete: "email", required: true });
  const submit = h("button", { class: "btn btn-primary btn-block", type: "submit" }, "Send reset link");

  const form = h("form", { class: "stack", onsubmit: async (event) => {
    event.preventDefault();
    error.classList.add("hidden");
    submit.disabled = true;
    try {
      const result = await api("/api/v1/auth/password-reset/request", { method: "POST", body: { email: email.value.trim() }, allowUnauthorized: true });
      done.textContent = result.message;
      done.classList.remove("hidden");
      form.classList.add("hidden");
    } catch (err) {
      error.textContent = err.message;
      error.classList.remove("hidden");
      submit.disabled = false;
    }
  } }, h("label", { class: "field" }, "Email", email), error, submit);

  mount(h("div", {}, siteNav(),
    h("div", { class: "auth-wrap" },
      h("div", { class: "card auth-card stack" },
        h("div", { class: "stack-sm" }, h("h1", {}, "Reset your password"), h("p", { class: "muted" }, "Enter your account email and we'll send you a reset link.")),
        form, done,
        h("p", { class: "muted small" }, link("/login", {}, "Back to sign in")),
      ),
    ),
  ));
}

async function viewResetPassword() {
  const token = new URLSearchParams(location.search).get("token") || "";
  history.replaceState(null, "", "/reset-password");

  const error = h("div", { class: "form-error hidden", role: "alert" });
  const password = h("input", { type: "password", autocomplete: "new-password", minlength: 10, required: true });
  const submit = h("button", { class: "btn btn-primary btn-block", type: "submit" }, "Set new password");

  const form = h("form", { class: "stack", onsubmit: async (event) => {
    event.preventDefault();
    error.classList.add("hidden");
    submit.disabled = true;
    try {
      await api("/api/v1/auth/password-reset/confirm", { method: "POST", body: { token, password: password.value }, allowUnauthorized: true });
      toast("Password updated. Sign in with your new password.");
      navigate("/login");
    } catch (err) {
      error.textContent = err.message;
      error.classList.remove("hidden");
      submit.disabled = false;
    }
  } },
    h("label", { class: "field" }, "New password", password, h("span", { class: "faint small" }, "At least 8 characters.")),
    error, submit);

  mount(h("div", {}, siteNav(),
    h("div", { class: "auth-wrap" },
      h("div", { class: "card auth-card stack" },
        h("h1", {}, "Choose a new password"),
        token ? form : h("div", { class: "form-error" }, "This reset link is missing its token. Request a new one."),
        h("p", { class: "muted small" }, link("/forgot-password", {}, "Request a new link")),
      ),
    ),
  ));
}

// ---------------------------------------------------------------------------
// Activation: a plan is required before the app opens
// ---------------------------------------------------------------------------

const FIT_CHECK_KEY = "janecareers.fitCheck";

// Since the Free plan (Nov 2026) nobody has to pick a plan to get in.
function needsActivation() {
  return false;
}

function afterActivationPath() {
  return "/app";
}

async function viewActivate() {
  if (state.user === undefined) await loadUser();
  if (!state.user) {
    navigate("/login", { replace: true });
    return;
  }

  const params = new URLSearchParams(location.search);
  const checkout = params.get("checkout");
  if (checkout) history.replaceState(null, "", "/app/activate");

  const body = h("div", { class: "container narrow stack" }, spinnerRow("Loading…"));
  mount(h("div", {},
    h("header", { class: "site-nav" }, h("div", { class: "container" }, brand(),
      h("button", { class: "btn btn-ghost btn-sm", onclick: logout }, "Sign out"))),
    h("main", { class: "activate" }, body),
  ));

  await Promise.all([loadBilling(), loadPlans(), loadPreferences().catch(() => null)]);

  if (checkout === "success") {
    body.replaceChildren(spinnerRow("Activating your account…"));
    // Stripe's webhook can land a moment after the redirect.
    for (let attempt = 0; attempt < 8 && !hasInterview(); attempt++) {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      await loadBilling();
    }
    if (hasInterview()) {
      toast("You're in. Jane is ready.");
      navigate(afterActivationPath(), { replace: true });
      return;
    }
    toast("Payment received. Your account is still activating. Refresh in a moment.", true);
  }
  if (checkout === "canceled") toast("No problem. Your card wasn't charged.");

  if (!needsActivation()) {
    navigate(afterActivationPath(), { replace: true });
    return;
  }

  const plansInfo = state.plans;
  const monthly = planFor("monthly");
  const trialEnd = new Date(Date.now() + ((monthly && monthly.trialDays) || 3) * 86400000);
  const billing = state.billing;
  const saved = readSavedFitCheck();

  const school = (() => {
    if (!billing.emailEnabled) {
      return h("p", { class: "muted small" }, "Is your school or program a Jane Careers partner? Sign up with your school email and ",
        link("/contact", {}, "contact us"), " to unlock access.");
    }
    if (billing.emailVerified) {
      return h("p", { class: "muted small" }, "Your email is confirmed, but your school doesn't have a Jane Careers license yet. Ask your career center to ",
        link("/contact", {}, "get in touch"), ".");
    }
    const send = h("button", { class: "btn btn-sm", onclick: async () => {
      send.disabled = true;
      try {
        await api("/api/v1/auth/verify-email/send", { method: "POST" });
        toast(`Check ${billing.email} for the confirmation link.`);
      } catch (error) {
        send.disabled = false;
        toast(error.message, true);
      }
    } }, "Confirm school email");
    return h("div", { class: "row-between" },
      h("p", { class: "muted small" }, `Signed up with a school email? If your school has a Jane Careers license, confirming ${billing.email} activates your account for free.`),
      send);
  })();

  body.replaceChildren(
    h("div", { class: "stack-sm center" },
      h("span", { class: "pill eyebrow" }, "One last step"),
      h("h1", { class: "onboarding-title" }, saved ? "Unlock your full Fit Check" : "Activate your account"),
      h("p", { class: "muted lead-sm" },
        trialAvailable() && monthly && monthly.trialDays
          ? `Start your ${monthly.trialDays}-day free trial. You won't be charged until ${formatDate(trialEnd.toISOString())}, and you can cancel in two clicks before then.`
          : "Choose a plan and Jane starts finding and scoring jobs for you right away. Cancel anytime, with a 3-day money-back guarantee."),
    ),
    plansInfo.billingEnabled ? h("div", { class: "grid-2 pricing-two" }, planCards(plansInfo, { compact: true })) : null,
    h("ul", { class: "ticks activate-ticks" },
      h("li", {}, "Every requirement checked against your real experience"),
      h("li", {}, "Gap Closer: what to add, what to learn, what to skip"),
      h("li", {}, "Direct links to the employer's own application page"),
    ),
    checkoutNote(),
  );
}

// ---------------------------------------------------------------------------
// Fit Check: paste a job, see whether to apply
// ---------------------------------------------------------------------------

function readSavedFitCheck() {
  try {
    const value = JSON.parse(sessionStorage.getItem(FIT_CHECK_KEY) || "null");
    return value && value.jobDescription ? value : null;
  } catch {
    return null;
  }
}

function saveFitCheck(input) {
  try { sessionStorage.setItem(FIT_CHECK_KEY, JSON.stringify(input)); } catch { /* storage full or blocked */ }
}

function clearSavedFitCheck() {
  try { sessionStorage.removeItem(FIT_CHECK_KEY); } catch { /* ignore */ }
}

// fitCheckTool renders the résumé + job form and the result.
// context is "public" (landing page) or "app" (signed in).
function fitCheckTool(context) {
  const saved = readSavedFitCheck();
  const result = h("div", { class: "fit-result-slot" });
  const error = h("div", { class: "form-error hidden", role: "alert" });

  let resumeTextValue = (saved && saved.resumeText) || "";
  let resumeLabel = (saved && saved.resumeName) || "";

  const resumeStatus = h("div", { class: "small muted", role: "status" });
  const showResumeStatus = () => resumeStatus.replaceChildren(resumeTextValue
    ? h("span", {}, "✓ ", resumeLabel || "Résumé added", h("span", { class: "faint" }, ` · ${resumeTextValue.split(/\s+/).length} words`))
    : "");

  // Résumé source: a saved résumé (signed in) or a file / pasted text.
  const resumes = context === "app" ? (state.resumes || []) : [];
  let picker = null;
  if (resumes.length) {
    picker = h("select", { "aria-label": "Résumé to check" },
      resumes.map((r) => h("option", { value: r.id }, r.filename)),
      h("option", { value: "" }, "Use a different file…"));
    const primary = resumes.find((r) => r.isPrimary) || resumes[0];
    picker.value = primary.id;
    if (saved && saved.resumeText) picker.value = "";
  }

  const fileInput = h("input", { type: "file", accept: "application/pdf,.pdf,.txt,text/plain", class: "visually-hidden", id: `fit-file-${Math.random().toString(36).slice(2)}` });
  const fileLabel = h("label", { class: "dropzone dropzone-compact", for: fileInput.id },
    h("span", { class: "dropzone-icon", "aria-hidden": "true" }, "⇪"),
    h("span", { class: "dropzone-title" }, "Upload your résumé (PDF)"),
    h("span", { class: "faint small" }, "Read in your browser. Nothing is saved until you sign up."),
  );
  const readFile = async (file) => {
    if (!file) return;
    fileLabel.classList.add("busy");
    resumeStatus.replaceChildren(spinnerRow(`Reading ${file.name}…`));
    try {
      resumeTextValue = await extractResumeText(file);
      resumeLabel = file.name;
      if (!resumeTextValue) throw new Error("Jane couldn't read any text in that PDF. If it's a scanned image, export it again as text, or paste it below.");
      showResumeStatus();
    } catch (err) {
      resumeTextValue = "";
      resumeStatus.replaceChildren(h("span", { class: "text-error" }, err.message));
    } finally {
      fileLabel.classList.remove("busy");
      fileInput.value = "";
    }
  };
  fileInput.addEventListener("change", () => readFile(fileInput.files[0]));
  fileLabel.addEventListener("dragover", (event) => { event.preventDefault(); fileLabel.classList.add("over"); });
  fileLabel.addEventListener("dragleave", () => fileLabel.classList.remove("over"));
  fileLabel.addEventListener("drop", (event) => { event.preventDefault(); fileLabel.classList.remove("over"); readFile(event.dataTransfer.files[0]); });

  const pasteResume = h("textarea", { rows: 6, maxlength: 40000, placeholder: "Or paste your résumé text here" });
  const pasteToggle = h("details", { class: "paste-toggle" }, h("summary", { class: "faint small" }, "Paste résumé text instead"), pasteResume);
  pasteResume.addEventListener("input", () => {
    resumeTextValue = pasteResume.value.trim();
    resumeLabel = "Pasted résumé";
    showResumeStatus();
  });

  const fileArea = h("div", { class: "stack-sm" }, fileInput, fileLabel, pasteToggle, resumeStatus);
  if (picker) {
    const syncPicker = () => fileArea.classList.toggle("hidden", picker.value !== "");
    picker.addEventListener("change", syncPicker);
    syncPicker();
  }
  showResumeStatus();

  const jobTitle = h("input", { type: "text", maxlength: 200, placeholder: "Job title (optional)", value: (saved && saved.jobTitle) || "" });
  const company = h("input", { type: "text", maxlength: 200, placeholder: "Company (optional)", value: (saved && saved.company) || "" });
  const job = h("textarea", { rows: 9, maxlength: 30000, placeholder: "Paste the full job posting, including the requirements or qualifications." });
  job.value = (saved && saved.jobDescription) || "";

  const submit = h("button", { class: "btn btn-primary btn-block", type: "submit" }, "Check my fit");

  const gatherResume = async () => {
    if (picker && picker.value) {
      const chosen = resumes.find((r) => r.id === picker.value);
      return { text: await resumeText(picker.value), name: chosen ? chosen.filename : "" };
    }
    return { text: resumeTextValue, name: resumeLabel };
  };

  const run = async () => {
    error.classList.add("hidden");
    const resume = await gatherResume();
    const input = {
      resumeText: resume.text,
      resumeName: resume.name,
      jobDescription: job.value.trim(),
      jobTitle: jobTitle.value.trim(),
      company: company.value.trim(),
    };
    const problems = [];
    if (!input.resumeText) problems.push("Add your résumé.");
    if (input.jobDescription.length < 200) problems.push("Paste the full job posting (at least a few paragraphs).");
    if (problems.length) {
      error.textContent = problems.join(" ");
      error.classList.remove("hidden");
      return;
    }

    submit.disabled = true;
    result.replaceChildren(h("div", { class: "card inset" }, spinnerRow("Jane is reading every requirement…")));
    result.scrollIntoView({ behavior: "smooth", block: "nearest" });
    try {
      const data = await api("/api/v1/fit-check", {
        method: "POST",
        allowUnauthorized: true,
        body: { resumeText: input.resumeText, jobDescription: input.jobDescription, jobTitle: input.jobTitle, company: input.company },
      });
      if (data.locked) saveFitCheck(input);
      else clearSavedFitCheck();
      result.replaceChildren(fitResult(data, input, { context, rerun: run }));
    } catch (err) {
      result.replaceChildren();
      if (err.code === "fit_check_limit") {
        saveFitCheck(input);
        result.replaceChildren(fitUnlockCard(err.message));
      } else {
        error.textContent = err.message;
        error.classList.remove("hidden");
      }
    } finally {
      submit.disabled = false;
    }
  };

  const form = h("form", { class: "stack fit-form", novalidate: true, onsubmit: (event) => { event.preventDefault(); run(); } },
    h("div", { class: "stack-sm" },
      h("span", { class: "field-label" }, "1. Your résumé"),
      picker,
      fileArea,
    ),
    h("div", { class: "stack-sm" },
      h("span", { class: "field-label" }, "2. The job"),
      h("div", { class: "grid-2" }, jobTitle, company),
      job,
    ),
    error,
    submit,
  );

  const node = h("div", { class: "fit-tool stack" }, h("div", { class: "card fit-card" }, form), result);

  // Coming back from checkout with a saved check: run it straight away.
  if (context === "app" && saved && isPro()) setTimeout(run, 0);

  return node;
}

function fitUnlockCard(message) {
  const signedIn = !!state.user;
  return h("div", { class: "card unlock stack-sm center" },
    h("h3", {}, "See every gap, and how to close it"),
    h("p", { class: "muted" }, message || "Jane found the requirements you're missing. Sign up to see them, with a plan for each one."),
    h("div", { class: "row center" },
      link(signedIn ? "/app/billing" : "/signup", { class: "btn btn-primary" }, signedIn ? "Upgrade to Pro" : "Sign up free")),
    h("p", { class: "faint small" }, "Your résumé and this job stay in your browser, so your full report is waiting after you sign up."),
  );
}

function fitLevelClass(data) {
  if (!data.scorable) return "low";
  return fitClass({ matchLevel: data.matchLevel });
}

function fitResult(data, input, { context, rerun }) {
  const cls = fitLevelClass(data);
  // CSP blocks inline style attributes; CSSOM property writes are allowed.
  const ring = h("div", { class: `score-ring ${cls}` }, h("span", {}, data.scorable ? `${data.matchPercentage}%` : "—"));
  ring.style.setProperty("--pct", String(Math.max(0, Math.min(100, data.matchPercentage || 0))));
  const header = h("div", { class: "fit-summary" },
    ring,
    h("div", { class: "stack-sm" },
      h("span", { class: `pill pill-${cls}` }, data.scorable ? data.matchLevel : "Can't score"),
      h("h3", { class: "verdict" }, data.verdict),
      data.scorable ? h("p", { class: "faint small" },
        `${data.supportedCount} met · ${data.partialCount} partial · ${data.missingCount} missing` +
        (data.requiredMissing ? ` · ${data.requiredMissing} must-have${data.requiredMissing === 1 ? "" : "s"} missing` : "")) : null,
    ),
  );

  const parts = [header];

  if (data.scorable && data.locked) {
    const teaser = Math.max(data.missingCount + data.partialCount, 3);
    parts.push(h("div", { class: "locked-wrap" },
      h("ul", { class: "reqs missing locked-blur", "aria-hidden": "true" },
        Array.from({ length: Math.min(teaser, 5) }, (_, i) => h("li", {}, ["Requirement Jane couldn't confirm on your résumé", "A certification this employer asks for", "Years of experience in a specific area", "A tool or system named in the posting", "A preferred qualification"][i % 5]))),
      fitUnlockCard(data.missingCount + data.partialCount
        ? `Jane found ${data.missingCount + data.partialCount} requirement${data.missingCount + data.partialCount === 1 ? "" : "s"} to work on. Sign up to see each one, with what to add, what to learn, and what to skip.`
        : "Sign up to see the full requirement-by-requirement breakdown."),
    ));
  } else if (data.scorable) {
    if (data.explanation) parts.push(h("p", { class: "muted" }, data.explanation));
    parts.push(reqList("supported", "You meet", data.supported));
    parts.push(gapCloser(data.gaps || [], {
      onAdded: rerun ? () => { toast("Added to your profile. Re-checking…"); rerun(); } : null,
    }));
    if (context === "app") {
      parts.push(applicationKitSection(async () => ({
        resumeText: input.resumeText,
        jobDescription: input.jobDescription,
        jobTitle: input.jobTitle,
        company: input.company,
        gaps: (data.gaps || []).map((g) => g.requirement),
      }), input.jobTitle));
    }
    if (context === "app") {
      parts.push(h("div", { class: "row" },
        link("/app/jobs", { class: "btn btn-sm" }, "Find more jobs like this"),
        input.jobTitle ? h("button", { class: "btn btn-sm", onclick: async (event) => {
          event.currentTarget.disabled = true;
          try {
            const key = `${input.jobTitle}|${input.company}`.toLowerCase();
            let hash = 0;
            for (const ch of key) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
            await trackMatch({ id: `fit-${hash.toString(36)}`, source: "fit-check", title: input.jobTitle, company: input.company || "Unknown company", description: input.jobDescription });
            toast("Added to My jobs");
          } catch (err) {
            toast(err.message, true);
          }
        } }, "Save to My jobs") : null,
      ));
    }
  }

  return h("div", { class: "card fit-result stack" }, parts);
}

async function viewFitCheckApp() {
  await loadResumes().catch(() => null);
  return h("div", {},
    pageHead("Check a job", "Found a job you like? Paste the posting and Jane tells you how well you match, what's missing, and whether to apply."),
    fitCheckTool("app"),
  );
}

// ---------------------------------------------------------------------------
// Gap Closer
// ---------------------------------------------------------------------------

const GAP_GROUPS = [
  ["add_if_true", "You may already have these", "Jane didn't see these on your résumé. If they're true, add them and your score goes up."],
  ["strengthen", "Make these clearer", "Your résumé hints at these. One specific example makes them count."],
  ["quick_win", "Quick wins", "Usually a short course or exam away."],
  ["real_gap", "Real gaps", "Experience, degrees or licenses you'd need to earn. Worth knowing before you apply."],
];

const GAP_EVIDENCE_CATEGORY = {
  skill: "skill", other: "skill", certification: "certification", license: "license",
  education: "education", experience: "experience",
};

// gapsFromMatch mirrors the server's Gap Closer rules for search results.
function gapsFromMatch(match) {
  const byText = new Map((match.requirements || []).map((r) => [r.text.toLowerCase().replace(/\s+/g, " ").trim(), r]));
  const find = (text) => byText.get(text.toLowerCase().replace(/\s+/g, " ").trim()) || { text, category: "other", importance: "required" };
  const gaps = [];
  for (const text of match.missingRequirements || []) gaps.push(classifyGapClient(find(text), false));
  for (const text of match.partialRequirements || []) gaps.push(classifyGapClient(find(text), true));
  return gaps;
}

function classifyGapClient(requirement, partial) {
  const gap = { requirement: requirement.text, category: requirement.category || "other", importance: requirement.importance || "required" };
  if (partial) return { ...gap, kind: "strengthen", advice: "Your résumé hints at this but doesn't show it clearly. Add a specific example (what you did, with what, and the result)." };
  switch (gap.category) {
    case "certification": return { ...gap, kind: "quick_win", advice: "If you hold this certification, add it. If not, it's often a short course or exam." };
    case "license": return { ...gap, kind: "real_gap", advice: "If you're licensed, add the license type and state. If not, this is usually a hard requirement." };
    case "education": return { ...gap, kind: "real_gap", advice: "If you have this degree or are enrolled, add it. If not, look for \"or equivalent experience\"." };
    case "experience": return { ...gap, kind: "real_gap", advice: "If you have this experience, spell out the years and role. If not, treat this job as a stretch." };
    case "physical":
    case "travel": return { ...gap, kind: "add_if_true", advice: "Résumés rarely mention this. If it's true for you, say so in your application or interview." };
    default: return { ...gap, kind: "add_if_true", advice: "Jane didn't see this on your résumé. If you've used it, add it with one real example." };
  }
}

async function addEvidenceToProfile(category, text) {
  const profile = await loadProfile(true);
  const draft = {
    headline: profile.headline, summary: profile.summary, targetRole: profile.targetRole, location: profile.location,
    evidence: (profile.evidence || []).map((e) => ({ category: e.category, text: e.text })),
  };
  draft.evidence.push({ category, text });
  await saveProfileDraft(draft);
  state.lastSearch = null;
}

function gapItem(gap, onAdded) {
  const evidenceCategory = GAP_EVIDENCE_CATEGORY[gap.category];
  const actions = h("div", { class: "row" });
  const item = h("li", { class: "gap-item" },
    h("div", { class: "gap-text" },
      h("span", {}, gap.requirement),
      gap.importance === "required" ? h("span", { class: "pill pill-low pill-xs" }, "Must-have") : h("span", { class: "pill pill-xs" }, "Nice to have")),
    h("p", { class: "faint small" }, gap.advice),
    actions,
  );

  if (evidenceCategory && state.user) {
    const open = h("button", { class: "btn btn-sm", type: "button", onclick: () => {
      const field = h("textarea", { rows: 2, maxlength: 2000 });
      field.value = gap.requirement;
      const save = h("button", { class: "btn btn-primary btn-sm", type: "button", onclick: async () => {
        const text = field.value.trim();
        if (!text) return;
        save.disabled = true;
        try {
          await addEvidenceToProfile(evidenceCategory, text);
          actions.replaceChildren(h("span", { class: "small text-good" }, "✓ Added to your profile"));
          if (onAdded) onAdded();
          else toast("Added to your profile. Run your search again to update scores.");
        } catch (err) {
          save.disabled = false;
          toast(err.message, true);
        }
      } }, "Add to profile");
      actions.replaceChildren(h("div", { class: "stack-sm gap-add" },
        h("span", { class: "faint small" }, "Describe where you've done this. Only add what's true: a recruiter will ask."),
        field,
        h("div", { class: "row" }, save, h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => actions.replaceChildren(open) }, "Cancel"))));
      field.focus();
    } }, gap.kind === "strengthen" ? "Add an example" : "I have this");
    actions.append(open);
  }
  return item;
}

function gapCloser(gaps, { onAdded } = {}) {
  if (!gaps.length) {
    return h("div", { class: "card inset stack-sm" },
      h("h3", {}, "Gap Closer"),
      h("p", { class: "muted" }, "No gaps. Jane found evidence for every requirement. Apply with confidence."));
  }
  return h("div", { class: "card inset stack gap-closer" },
    h("div", { class: "stack-sm" },
      h("h3", {}, "Gap Closer"),
      h("p", { class: "muted small" }, "Every requirement Jane couldn't confirm, and what to do about it."),
    ),
    GAP_GROUPS.map(([kind, title, hint]) => {
      const items = gaps.filter((g) => g.kind === kind);
      if (!items.length) return null;
      return h("div", { class: `stack-sm gap-group gap-${kind}` },
        h("span", { class: "section-label" }, `${title} (${items.length})`),
        h("p", { class: "faint small" }, hint),
        h("ul", { class: "gap-list" }, items.map((gap) => gapItem(gap, onAdded))),
      );
    }),
  );
}

// ---------------------------------------------------------------------------
// Application Kit: a truth-locked tailored résumé for one job
// ---------------------------------------------------------------------------

function kitPlainText(kit, title) {
  const lines = [];
  if (title) lines.push(`Application Kit: ${title}`, "");
  if (kit.headline) lines.push(kit.headline, "");
  if (kit.summary) lines.push(kit.summary, "");
  if (kit.bullets.length) lines.push(...kit.bullets.map((b) => `• ${b.text}`), "");
  if (kit.keywords.length) lines.push(`Keywords: ${kit.keywords.join(", ")}`, "");
  if (kit.interview.length) {
    lines.push("Interview prep:");
    kit.interview.forEach((q, i) => lines.push(`${i + 1}. ${q.question}`, `   ${q.approach}`));
  }
  return lines.join("\n").trim();
}

async function copyText(text, label) {
  try {
    await navigator.clipboard.writeText(text);
    toast(`${label || "Copied"} to your clipboard`);
  } catch {
    toast("Couldn't copy. Select the text and copy it instead.", true);
  }
}

function renderKit(kit, title, input) {
  const copyBtn = (text, label) => h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => copyText(text, label) }, "Copy");
  const parts = [
    h("div", { class: "row-between" },
      h("div", { class: "stack-sm" },
        h("h3", {}, "Your Application Kit"),
        h("span", { class: "truth-badge" }, "✓ Built only from your real experience")),
      h("button", { class: "btn btn-primary btn-sm", type: "button", onclick: () => copyText(kitPlainText(kit, title), "Kit copied") }, "Copy everything")),
  ];
  if (kit.headline || kit.summary) {
    parts.push(h("div", { class: "kit-block stack-sm" },
      h("div", { class: "row-between" }, h("span", { class: "section-label" }, "Headline & summary"), copyBtn([kit.headline, kit.summary].filter(Boolean).join("\n\n"), "Summary copied")),
      kit.headline ? h("p", { class: "kit-headline" }, kit.headline) : null,
      kit.summary ? h("p", {}, kit.summary) : null));
  }
  if (kit.bullets.length) {
    parts.push(h("div", { class: "kit-block stack-sm" },
      h("div", { class: "row-between" }, h("span", { class: "section-label" }, "Tailored bullets, most relevant first"), copyBtn(kit.bullets.map((b) => `• ${b.text}`).join("\n"), "Bullets copied")),
      h("ul", { class: "kit-bullets" }, kit.bullets.map((b) => h("li", {},
        h("span", {}, b.text),
        h("details", { class: "kit-source" }, h("summary", {}, "From your résumé"), h("p", { class: "faint small" }, `“${b.source}”`)))))));
  }
  if (kit.droppedLines) {
    parts.push(h("p", { class: "faint small" }, `Jane removed ${kit.droppedLines} line${kit.droppedLines === 1 ? "" : "s"} she couldn't trace to your résumé. She'd rather leave something out than make it up.`));
  }
  if (kit.keywords.length) {
    parts.push(h("div", { class: "kit-block stack-sm" },
      h("span", { class: "section-label" }, "Use these words from the posting"),
      h("div", { class: "row" }, kit.keywords.map((k) => h("span", { class: "pill" }, k)))));
  }
  if (kit.interview.length) {
    parts.push(h("div", { class: "kit-block stack-sm" },
      h("span", { class: "section-label" }, "Interview prep"),
      h("p", { class: "faint small" }, "Practice out loud, then type your answer and Jane will tell you how to improve it."),
      h("ol", { class: "kit-interview" }, kit.interview.map((q) => h("li", {},
        h("strong", {}, q.question),
        h("p", { class: "muted small" }, q.approach),
        input ? practiceBox(q.question, input) : null)))));
  }
  return h("div", { class: "card inset stack kit" }, parts);
}

// applicationKitSection returns a card with a "Build my Application Kit"
// button. getInput resolves { resumeText, jobDescription, jobTitle, company, gaps }.
function applicationKitSection(getInput, title, { withLetter = true } = {}) {
  const slot = h("div", {});
  const start = h("button", { class: "btn btn-primary", type: "button", onclick: async () => {
    start.disabled = true;
    slot.replaceChildren(h("div", { class: "card inset" }, spinnerRow("Jane is tailoring your résumé to this job…")));
    let input;
    try {
      input = await getInput();
      const kit = await api("/api/v1/application-kit", { method: "POST", body: input });
      slot.replaceChildren(renderKit(kit, title, input));
      intro.remove();
    } catch (error) {
      start.disabled = false;
      slot.replaceChildren(h("div", { class: "form-error" }, error.code === "pro_required" ? "Choose a plan to build Application Kits." : error.message));
    }
  } }, "Build my Application Kit");
  const intro = h("div", { class: "card inset kit-intro stack-sm" },
    h("h3", {}, "Ready to apply? Let Jane tailor your résumé"),
    h("p", { class: "muted small" }, "A new summary, bullets and keywords for this job that you can copy into your own résumé, plus tips for anything you're missing."),
    h("div", { class: "row" }, start));
  return h("div", { class: "stack-sm" }, intro, slot, withLetter ? coverLetterSection(getInput) : null);
}

// tailoredResumeSection rewrites the whole résumé for one job (Pro) and
// offers it as a PDF or Word file.
function tailoredResumeSection(getInput) {
  const slot = h("div", {});
  let company = "";
  const preview = (resume) => h("div", { class: "try-letter-paper tr-paper" },
    h("p", { class: "tr-name" }, resume.name || ""),
    (resume.contact || []).length ? h("p", { class: "faint small" }, resume.contact.join(" · ")) : null,
    resume.summary ? h("p", {}, resume.summary) : null,
    (resume.experience || []).map((role) => h("div", { class: "tr-role" },
      h("p", {}, h("b", {}, [role.title, role.company].filter(Boolean).join(", ")), role.dates ? h("span", { class: "faint small" }, ` · ${role.dates}`) : null),
      h("ul", {}, (role.bullets || []).map((b) => h("li", {}, b))))),
    (resume.skills || []).length ? h("p", { class: "small" }, h("b", {}, "Skills: "), resume.skills.join(", ")) : null);
  const make = h("button", { class: "btn btn-primary", type: "button", onclick: async () => {
    make.disabled = true;
    slot.replaceChildren(h("div", { class: "card inset" }, spinnerRow("Jane is rewriting your résumé for this job… (about 30 seconds)")));
    try {
      const input = await getInput();
      company = input.company || "";
      const resume = await api("/api/v1/tailored-resume", { method: "POST", body: {
        resumeText: input.resumeText, jobDescription: input.jobDescription, jobTitle: input.jobTitle, company: input.company } });
      const error = h("div", { class: "form-error", hidden: true });
      slot.replaceChildren(h("div", { class: "card inset stack-sm" },
        h("div", { class: "row-between" }, h("h3", {}, "Your tailored résumé"),
          h("div", { class: "row doc-actions" }, downloadButtons("/api/v1/documents/resume", () => ({ resume, company }),
            (e) => { error.hidden = false; error.textContent = e.message || "Couldn't download the file."; }))),
        error,
        preview(resume),
        h("p", { class: "faint small" }, "Give it a quick read before you send it. Download the Word file to edit.")));
      make.textContent = "Rewrite it again";
    } catch (error) {
      slot.replaceChildren(error.code === "pro_required" || error.code === "plan_limit_reached"
        ? freeUpgrade("Tailored résumés for every job", error.message)
        : h("div", { class: "form-error" }, error.message));
    } finally {
      make.disabled = false;
    }
  } }, "Make my tailored résumé");
  return h("div", { class: "stack-sm" },
    h("div", { class: "card inset stack-sm" },
      h("h3", {}, "A résumé made for this job"),
      h("p", { class: "muted small" }, "Jane rewrites your whole résumé for this job, in its language, and gives it to you as a ready-to-send PDF or an editable Word file."),
      h("div", { class: "row" }, make)),
    slot);
}

// coverLetterSection writes a short, truth-locked cover letter for the job.
function coverLetterSection(getInput) {
  const slot = h("div", {});
  const write = h("button", { class: "btn", type: "button", onclick: async () => {
    write.disabled = true;
    slot.replaceChildren(h("div", { class: "card inset" }, spinnerRow("Jane is writing your cover letter…")));
    try {
      const input = await getInput();
      const letter = await api("/api/v1/cover-letter", { method: "POST", body: {
        resumeText: input.resumeText, jobDescription: input.jobDescription, jobTitle: input.jobTitle, company: input.company } });
      slot.replaceChildren(h("div", { class: "card inset stack-sm" },
        h("div", { class: "row-between" }, h("h3", {}, "Your cover letter"),
          h("div", { class: "row doc-actions" },
            h("button", { class: "btn btn-primary btn-sm", type: "button", onclick: () => copyText(letter.text, "Cover letter copied") }, "Copy"),
            downloadButtons("/api/v1/documents/cover-letter", () => ({
              name: letter.name || "", greeting: letter.greeting || "", paragraphs: letter.paragraphs || [], closing: letter.closing || "",
              company: input.company || "", resumeText: (input.resumeText || "").slice(0, 3000) }),
            (error) => toast(error.message || "Couldn't download the file.")))),
        h("div", { class: "try-letter-paper" }, letter.text.split("\n\n").map((part) => h("p", {}, part.split("\n").flatMap((line, i) => i ? [h("br", {}), line] : [line])))),
        h("p", { class: "faint small" }, "Give it a quick read and add anything personal before you send it. Download the Word file to edit.")));
      write.textContent = "Write it again";
      write.disabled = false;
    } catch (error) {
      write.disabled = false;
      slot.replaceChildren(error.code === "pro_required" || error.code === "free_limit_reached" || error.code === "plan_limit_reached"
        ? freeUpgrade("A cover letter for every job", error.message)
        : h("div", { class: "form-error" }, error.message));
    } finally {
      if (isFree()) loadBilling();
    }
  } }, "Write my cover letter");
  return h("div", { class: "stack-sm" },
    h("div", { class: "card inset stack-sm" },
      h("h3", {}, "Your cover letter"),
      h("p", { class: "muted small" }, "Jane writes a short letter for this job that shows why you're a fit, in a natural voice. You can copy it or download it."),
      isFree() ? h("p", { class: "faint small" }, "Cover letters are part of Prep and Pro.") : null,
      h("div", { class: "row" }, write)),
    slot);
}

// ---------------------------------------------------------------------------
// Home: Jane finds and scores jobs automatically
// ---------------------------------------------------------------------------

// searchMatches runs a job search with the user's profile and default
// résumé and remembers it for this session.
async function searchMatches({ targetRole, location }) {
  const [profile, preferences, resumes] = await Promise.all([loadProfile(), loadPreferences(), loadResumes()]);
  const primary = (resumes || []).find((r) => r.isPrimary) || (resumes || [])[0];
  const anyStyle = !preferences.remote && !preferences.hybrid && !preferences.onsite;
  const request = {
    careerProfile: profileToSearchProfile(profile),
    targetRole, location,
    minimumSalary: preferences.minimumSalary || "",
    remote: anyStyle || !!preferences.remote,
    hybrid: anyStyle || !!preferences.hybrid,
    onsite: anyStyle || !!preferences.onsite,
  };
  if (primary) request.resumeText = await resumeText(primary.id);
  const page = await fetchMatchPage(request, 0);
  const { careerProfile, resumeText: _omit, ...shown } = request;
  state.lastSearch = { request: shown, fullRequest: request, matches: page.matches, total: page.total, nextOffset: page.nextOffset, locked: page.locked, resumeId: primary ? primary.id : "" };
  return state.lastSearch;
}

async function saveSearchTarget(targetRole, location) {
  const profile = await loadProfile(true);
  await saveProfileDraft({
    headline: profile.headline, summary: profile.summary, targetRole, location,
    evidence: (profile.evidence || []).map((e) => ({ category: e.category, text: e.text })),
  });
}

async function viewHome() {
  const [profile, resumes, applications] = await Promise.all([
    loadProfile().catch(() => ({})),
    loadResumes().catch(() => []),
    api("/api/v1/applications").catch(() => []),
  ]);

  const name = ((state.user && (state.user.displayName || state.user.display_name)) || "").trim().split(/\s+/)[0];
  const count = (statuses) => (applications || []).filter((a) => statuses.includes(a.status)).length;
  const tracker = h("div", { class: "home-tracker row" },
    link("/app/applications", { class: "tracker-stat" }, h("strong", {}, String(count(["saved"]))), h("span", {}, "Saved")),
    link("/app/applications", { class: "tracker-stat" }, h("strong", {}, String(count(["applied", "received", "recruiter_contact"]))), h("span", {}, "Applied")),
    link("/app/applications", { class: "tracker-stat" }, h("strong", {}, String(count(["interviewing"]))), h("span", {}, "Interviewing")),
    link("/app/applications", { class: "tracker-stat" }, h("strong", {}, String(count(["offer", "hired"]))), h("span", {}, "Offers")),
  );

  const head = h("div", { class: "page-head" },
    h("h1", {}, name ? `Hi ${name}, here are your matches` : "Your matches"),
    h("p", {}, "Jane searches open jobs and scores every one against your résumé, automatically."));

  // No résumé yet: that's the only thing Jane needs first.
  if (!(resumes || []).length && !((profile.evidence || []).length)) {
    return h("div", { class: "stack home" }, head,
      h("div", { class: "card stack" },
        h("h3", {}, "Add your résumé and Jane will start finding jobs"),
        resumeDropzone(() => navigate("/app", { replace: true })),
      ));
  }

  const results = h("div", { class: "stack" });
  const last = state.lastSearch && state.lastSearch.request;
  const targetRole = (last && last.targetRole) || profile.targetRole || "";
  const city = (last && last.location) || profile.location || "";

  const showMatches = (search) => {
    const matches = (search.matches || []).slice(0, 5);
    const label = `${search.request.targetRole} · ${search.request.location}`;
    results.replaceChildren(
      h("div", { class: "row-between" },
        h("span", { class: "muted" }, matches.length ? `Top ${matches.length} for ${label}` : `No matches yet for ${label}`),
        h("div", { class: "row" },
          h("button", { class: "btn btn-ghost btn-sm", onclick: () => askTarget(search.request.targetRole, search.request.location) }, "Change"),
          (search.total || (search.matches || []).length) > 5 ? link("/app/jobs", { class: "btn btn-sm" }, `See all ${search.total || search.matches.length}`) : null)),
      matches.length
        ? h("div", { class: "stack" }, matches.map(jobCard), search.locked ? lockedMatchesCard(search.locked) : null, sourceCredit(matches))
        : h("div", { class: "card empty" },
            h("h3", {}, "Nothing close enough yet"),
            h("p", {}, "Try a broader job title or a bigger nearby city. Jane only shows jobs she could actually score."),
            h("button", { class: "btn", onclick: () => askTarget(search.request.targetRole, search.request.location) }, "Change search")),
    );
  };

  const run = async (role, where) => {
    results.replaceChildren(h("div", { class: "card" }, spinnerRow(`Jane is finding ${role} jobs in ${where} and scoring each one…`)));
    try {
      showMatches(await searchMatches({ targetRole: role, location: where }));
    } catch (error) {
      results.replaceChildren(error.code === "pro_required" || error.code === "free_limit_reached"
        ? paywall(error.code === "free_limit_reached" ? "You've used today's free searches" : "Your matches are ready", error.code === "free_limit_reached" ? error.message : "Choose a plan to see every job ranked by how well you fit.")
        : h("div", { class: "form-error" }, error.message));
    }
  };

  // The only question Jane asks: what job, and where.
  const askTarget = (role = "", where = "") => {
    const roleInput = h("input", { type: "text", maxlength: 200, placeholder: "e.g. Registered Nurse", value: role });
    const cityInput = h("input", { type: "text", maxlength: 200, placeholder: "City, town or ZIP (e.g. Ocala, FL or Toronto, ON)", value: where });
    const go = h("button", { class: "btn btn-primary", type: "submit" }, "Find my matches");
    results.replaceChildren(h("form", { class: "card stack", onsubmit: async (event) => {
      event.preventDefault();
      const r = roleInput.value.trim(); const c = cityInput.value.trim();
      if (!r || !c) { toast("Add a job title and a city.", true); return; }
      go.disabled = true;
      saveSearchTarget(r, c).catch(() => {});
      await run(r, c);
    } },
      h("h3", {}, "What job are you looking for?"),
      h("div", { class: "grid-2" },
        h("label", { class: "field" }, "Job title", roleInput),
        h("label", { class: "field" }, "City", cityInput)),
      h("div", { class: "row" }, go),
    ));
    roleInput.focus();
  };

  if (state.lastSearch) showMatches(state.lastSearch);
  else if (targetRole && city) setTimeout(() => run(targetRole, city), 0);
  else setTimeout(() => askTarget(targetRole, city), 0);

  return h("div", { class: "stack home" },
    head,
    results,
    h("div", { class: "card stack-sm" },
      h("div", { class: "row-between" }, h("h3", {}, "My applications"), link("/app/applications", { class: "btn btn-ghost btn-sm" }, "Open tracker →")),
      tracker),
    h("p", { class: "faint small" },
      "Have one specific job in mind? ", link("/app/fit-check", {}, "Check it here"),
      ". Need help? Call ", h("a", { href: "tel:+16468212027" }, "646-821-2027"), "."),
  );
}

// ---------------------------------------------------------------------------
// Spoken interview practice
// ---------------------------------------------------------------------------

const SpeechRecognitionImpl = window.SpeechRecognition || window.webkitSpeechRecognition;
const FILLER_PATTERNS = [["um", /\b(um+|umm+)\b/g], ["uh", /\b(uh+|erm)\b/g], ["you know", /\byou know\b/g], ["kind of", /\bkind of\b/g], ["sort of", /\bsort of\b/g], ["basically", /\bbasically\b/g], ["actually", /\bactually\b/g], ["literally", /\bliterally\b/g], ["I mean", /\bi mean\b/g]];

function formatClock(seconds) {
  const s = Math.max(0, Math.round(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

// deliveryStats scores how an answer was spoken: length, pace and fillers.
function deliveryStats(text, seconds) {
  const lower = text.toLowerCase();
  const words = (lower.match(/[a-z0-9']+/g) || []).length;
  const fillers = [];
  for (const [label, pattern] of FILLER_PATTERNS) {
    const count = (lower.match(pattern) || []).length;
    if (count) fillers.push([label, count]);
  }
  const fillerCount = fillers.reduce((sum, [, n]) => sum + n, 0);
  const minutes = Math.max(seconds / 60, 0.1);
  const wpm = Math.round(words / minutes);
  const length = seconds < 30 ? ["low", "Too short", "Aim for 1 to 2 minutes. Add the result and what you learned."]
    : seconds > 150 ? ["mid", "A bit long", "Tighten it to about 2 minutes. Lead with the point."]
    : ["good", "Good length", "Right in the 1 to 2 minute sweet spot."];
  const pace = wpm > 175 ? ["mid", "Fast", "Slow down a little and pause between points."]
    : wpm < 105 ? ["mid", "Slow", "Pick up the pace a bit so it sounds confident."]
    : ["good", "Good pace", "Easy to follow."];
  const perMinute = fillerCount / minutes;
  const filler = perMinute > 4 ? ["low", "Lots of filler", "Pause silently instead of saying “um” or “you know”."]
    : perMinute > 2 ? ["mid", "Some filler", "A few fillers. Try a short pause instead."]
    : ["good", "Clean", "Barely any filler words."];
  return { words, wpm, seconds, fillers, fillerCount, length, pace, filler };
}

function deliveryCard(stats) {
  const tile = (title, value, [cls, label, tip]) => h("div", { class: `delivery-tile ${cls}` },
    h("span", { class: "hero-kicker" }, title), h("b", {}, value), h("span", { class: `small demo-${cls}` }, label), h("span", { class: "faint small" }, tip));
  return h("div", { class: "delivery stack-sm" },
    h("span", { class: "section-label" }, "Your delivery"),
    h("div", { class: "delivery-grid" },
      tile("Length", formatClock(stats.seconds), stats.length),
      tile("Pace", `${stats.wpm} wpm`, stats.pace),
      tile("Filler words", String(stats.fillerCount), stats.filler)),
    stats.fillers.length ? h("p", { class: "faint small" }, "Heard: ", stats.fillers.map(([label, n]) => `“${label}” ×${n}`).join(", ")) : null);
}

function renderAnswerFeedback(feedback) {
  return h("div", { class: "practice-feedback stack-sm" },
    feedback.strengths.length ? h("div", {}, h("span", { class: "section-label" }, "What works"),
      h("ul", { class: "reqs supported" }, feedback.strengths.map((item) => h("li", {}, item)))) : null,
    feedback.improvements.length ? h("div", {}, h("span", { class: "section-label" }, "How to improve it"),
      h("ul", { class: "reqs partial" }, feedback.improvements.map((item) => h("li", {}, item)))) : null,
    feedback.strongerAnswer ? h("div", { class: "stack-sm" },
      h("div", { class: "row-between" }, h("span", { class: "section-label" }, "A stronger version"),
        h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => copyText(feedback.strongerAnswer, "Answer copied") }, "Copy")),
      h("p", { class: "stronger-answer" }, feedback.strongerAnswer),
      h("p", { class: "faint small" }, "Built only from your answer and résumé. Fill in anything in [brackets] with your real details.")) : null);
}

// practiceBox lets the user answer one interview question out loud (or by
// typing) and get Jane's coaching on delivery and content.
function practiceBox(question, input, { open = false } = {}) {
  const deliverySlot = h("div", {});
  const slot = h("div", { class: "practice-result" });
  const answer = h("textarea", { rows: 4, maxlength: 6000, placeholder: "Answer out loud with the mic, or type your answer the way you'd say it." });
  const clock = h("span", { class: "rec-clock" }, "0:00");
  const level = h("span", { class: "rec-level" }, h("i"));
  const recBar = h("div", { class: "rec-bar hidden" }, h("span", { class: "rec-dot" }), h("span", {}, "Listening"), clock, level);

  let lastSpoken = null;
  let attempt = 0;
  // Earlier attempts stay below, folded, so the user can compare.
  const history = h("div", { class: "stack-sm" });

  // retry keeps this attempt for comparison, clears the answer and starts
  // recording again (or puts the cursor back in the box when typing).
  const retry = (feedback) => {
    const spoken = answer.value.trim();
    const kept = h("details", { class: "card practice-attempt" },
      h("summary", {}, `Attempt ${attempt}${lastSpoken ? ` · ${formatClock(lastSpoken.seconds)}` : ""}`),
      h("div", { class: "stack-sm" },
        h("p", { class: "muted" }, spoken),
        deliverySlot.firstChild ? deliverySlot.firstChild.cloneNode(true) : null,
        renderAnswerFeedback(feedback)));
    history.prepend(kept);
    answer.value = "";
    lastSpoken = null;
    deliverySlot.replaceChildren();
    slot.replaceChildren();
    if (mic) mic.click();
    else answer.focus();
  };

  const getFeedback = async () => {
    const text = answer.value.trim();
    if (text.length < 40) {
      slot.replaceChildren(h("p", { class: "text-error small" }, "Say or write a few sentences so Jane has something to coach."));
      return;
    }
    submit.disabled = true;
    slot.replaceChildren(spinnerRow("Jane is reviewing your answer…"));
    try {
      const feedback = await api("/api/v1/interview-feedback", {
        method: "POST",
        body: { question, answer: text, resumeText: input.resumeText, jobDescription: input.jobDescription, jobTitle: input.jobTitle || "", stage: input.stage || "" },
      });
      attempt += 1;
      const tryAgain = h("button", { class: "btn btn-primary btn-sm", type: "button", onclick: () => retry(feedback) }, mic ? "🎙 Try again" : "Try again");
      slot.replaceChildren(renderAnswerFeedback(feedback),
        h("div", { class: "row try-again" }, tryAgain,
          h("span", { class: "faint small" }, "Use Jane's tips and record your answer again.")));
    } catch (error) {
      slot.replaceChildren(error.code === "free_limit_reached"
        ? freeUpgrade("Keep practicing with Jane", error.message)
        : h("p", { class: "text-error small" }, error.code === "pro_required" ? "Choose a plan to practice with Jane." : error.message));
    } finally {
      submit.disabled = false;
    }
  };

  const submit = h("button", { class: "btn btn-sm", type: "button", onclick: () => {
    if (lastSpoken && answer.value.trim() !== lastSpoken.text) deliverySlot.replaceChildren();
    getFeedback();
  } }, "Get feedback");

  let mic = null;
  if (SpeechRecognitionImpl) {
    let recognition = null, started = 0, ticker = null, audio = null, finalText = "";
    const stopAudio = () => {
      if (audio) { audio.stream.getTracks().forEach((t) => t.stop()); audio.context.close().catch(() => {}); audio = null; }
    };
    const startLevel = async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        const context = new (window.AudioContext || window.webkitAudioContext)();
        const analyser = context.createAnalyser();
        analyser.fftSize = 256;
        context.createMediaStreamSource(stream).connect(analyser);
        const data = new Uint8Array(analyser.frequencyBinCount);
        audio = { stream, context };
        const draw = () => {
          if (!audio) return;
          analyser.getByteTimeDomainData(data);
          let peak = 0;
          for (const v of data) peak = Math.max(peak, Math.abs(v - 128));
          level.firstChild.style.setProperty("--lvl", String(Math.min(1, peak / 64)));
          requestAnimationFrame(draw);
        };
        draw();
      } catch { /* the meter is optional */ }
    };
    const stop = () => {
      if (!recognition) return;
      recognition.onend = null;
      recognition.stop();
      recognition = null;
      clearInterval(ticker);
      stopAudio();
      recBar.classList.add("hidden");
      mic.classList.remove("recording");
      mic.textContent = "🎙 Answer again";
      const seconds = (Date.now() - started) / 1000;
      const text = answer.value.trim();
      if (text) {
        lastSpoken = { text, seconds };
        deliverySlot.replaceChildren(deliveryCard(deliveryStats(text, seconds)));
        getFeedback();
      }
    };
    mic = h("button", { class: "btn btn-primary btn-sm mic-btn", type: "button", onclick: () => {
      if (recognition) { stop(); return; }
      recognition = new SpeechRecognitionImpl();
      recognition.continuous = true;
      recognition.interimResults = true;
      recognition.lang = "en-US";
      finalText = "";
      answer.value = "";
      deliverySlot.replaceChildren();
      slot.replaceChildren();
      recognition.onresult = (event) => {
        let interim = "";
        for (let i = event.resultIndex; i < event.results.length; i++) {
          const piece = event.results[i][0].transcript;
          if (event.results[i].isFinal) finalText += piece + " ";
          else interim += piece;
        }
        answer.value = (finalText + interim).trim();
      };
      recognition.onerror = (event) => {
        if (event.error === "not-allowed" || event.error === "service-not-allowed") {
          toast("Allow microphone access in your browser to answer out loud.", true);
          stop();
        }
      };
      // Browsers end recognition after a silence; keep listening until the
      // user presses stop.
      recognition.onend = () => { if (recognition) { try { recognition.start(); } catch { /* already running */ } } };
      try { recognition.start(); } catch { toast("Couldn't start the microphone. Try again.", true); recognition = null; return; }
      started = Date.now();
      clock.textContent = "0:00";
      ticker = setInterval(() => { clock.textContent = formatClock((Date.now() - started) / 1000); }, 500);
      recBar.classList.remove("hidden");
      mic.classList.add("recording");
      mic.textContent = "■ Stop and get feedback";
      startLevel();
    } }, "🎙 Answer out loud");
  }

  const box = h("details", { class: "practice" },
    h("summary", {}, "Practice this answer"),
    h("div", { class: "stack-sm" },
      recBar, answer,
      h("div", { class: "row" }, mic, submit,
        mic ? null : h("span", { class: "faint small" }, "Voice answers work in Chrome, Edge and Safari.")),
      deliverySlot, slot, history));
  if (open) box.open = true;
  return box;
}


// ---------------------------------------------------------------------------
// Interview practice page
// ---------------------------------------------------------------------------

// Interview types to practice for (matches tailor.InterviewStages).
const INTERVIEW_STAGES = [
  ["mixed", "Not sure yet: the most likely questions"],
  ["phone", "First introduction: phone screen with a recruiter"],
  ["first", "First round: with the hiring manager"],
  ["behavioral", "Behavioral round (“Tell me about a time…”)"],
  ["role", "Technical or skills interview"],
  ["design", "System design interview"],
  ["panel", "Panel interview"],
  ["final", "Final round"],
  ["ask", "Questions to ask the interviewer"],
];

async function viewInterviewPractice() {
  const [resumes, applications] = await Promise.all([loadResumes().catch(() => []), api("/api/v1/applications").catch(() => [])]);
  const results = h("div", { class: "stack" });
  const head = pageHead("Interview practice", "Add the job and company you're interviewing with, then answer Jane's likely questions out loud. She scores your delivery and coaches your answer.");

  if (!(resumes || []).length) {
    return h("div", { class: "stack" }, head, h("div", { class: "card empty" },
      h("h3", {}, "Add your résumé first"),
      h("p", {}, "Jane uses it to predict questions and coach your answers with your real experience."),
      link("/app/resumes", { class: "btn btn-primary" }, "Upload résumé")));
  }

  const resumeSelect = h("select", { "aria-label": "Résumé" }, resumes.map((r) => h("option", { value: r.id }, r.filename)));
  resumeSelect.value = (resumes.find((r) => r.isPrimary) || resumes[0]).id;

  const saved = (applications || []).filter((a) => a.job);
  const jobSelect = h("select", { "aria-label": "Job to practice for" },
    saved.map((a) => h("option", { value: a.jobId }, `${a.job.title} · ${a.job.company}`)),
    h("option", { value: "" }, "Paste a job posting instead…"));
  if (!saved.length) jobSelect.value = "";
  // "Practice interview" on My applications opens this page with the job picked.
  const wantedJob = new URLSearchParams(location.search).get("job");
  if (wantedJob && saved.some((a) => a.jobId === wantedJob)) jobSelect.value = wantedJob;

  const pasteTitle = h("input", { type: "text", maxlength: 200, placeholder: "e.g. Marketing Manager", "aria-label": "Job title" });
  const pasteCompany = h("input", { type: "text", maxlength: 200, placeholder: "e.g. Brightline Foods", "aria-label": "Company" });
  const pasteBody = h("textarea", { rows: 6, maxlength: 30000, placeholder: "Paste the full job posting", "aria-label": "Job posting" });
  const pasteArea = h("div", { class: "stack-sm" },
    h("div", { class: "grid-2" },
      h("label", { class: "field" }, "Job title", pasteTitle),
      h("label", { class: "field" }, "Company you're interviewing with", pasteCompany)),
    h("label", { class: "field" }, "Job posting", pasteBody));
  // "Practice this interview" from a job's steps opens this page with the job filled in.
  if (state.practiceJob) {
    jobSelect.value = "";
    pasteTitle.value = state.practiceJob.title || "";
    pasteCompany.value = state.practiceJob.company || "";
    pasteBody.value = state.practiceJob.description || "";
    state.practiceStage = state.practiceJob.stage || "";
    state.practiceJob = null;
  }
  const syncPaste = () => pasteArea.classList.toggle("hidden", jobSelect.value !== "");
  jobSelect.addEventListener("change", syncPaste);
  syncPaste();

  const stageSelect = h("select", { "aria-label": "Interview stage" },
    INTERVIEW_STAGES.map(([value, label]) => h("option", { value }, label)));
  if (state.practiceStage) {
    if (INTERVIEW_STAGES.some(([value]) => value === state.practiceStage)) stageSelect.value = state.practiceStage;
    state.practiceStage = "";
  }

  // One practice session: the request that made it, every question asked so
  // far (so "More questions" never repeats), and where new ones go.
  let session = null;
  const list = h("div", { class: "stack" });
  const more = h("button", { class: "btn", type: "button", onclick: () => loadQuestions(true) }, "More questions");
  const moreRow = h("div", { class: "row center" }, more);

  const renderQuestions = (questions) => {
    for (const q of questions) {
      const number = session.asked.length + 1;
      session.asked.push(q.question);
      const askThem = session.stage === "ask";
      list.append(h("div", { class: "card stack-sm practice-q" },
        h("div", { class: "row" }, h("span", { class: "step-num", "aria-hidden": "true" }, String(number)), h("strong", {}, q.question)),
        h("p", { class: "muted small" }, askThem ? h("span", {}, h("strong", {}, "Why ask it: "), q.approach) : q.approach),
        askThem ? null : practiceBox(q.question, session.input, { open: number === 1 })));
    }
  };

  async function loadQuestions(again) {
    const button = again ? more : go;
    button.disabled = true;
    const waiting = h("div", { class: "card" }, spinnerRow(again ? "Jane is writing new questions…" : "Jane is predicting what they'll ask you…"));
    if (again) moreRow.replaceChildren(waiting);
    else results.replaceChildren(waiting);
    try {
      if (!again) {
        const resume = await resumeText(resumeSelect.value);
        const body = jobSelect.value
          ? { resumeText: resume, jobId: jobSelect.value }
          : { resumeText: resume, jobTitle: pasteTitle.value.trim(), company: pasteCompany.value.trim(), jobDescription: pasteBody.value.trim() };
        session = { body: { ...body, stage: stageSelect.value }, stage: stageSelect.value, asked: [], input: null };
      }
      const data = await api("/api/v1/interview-questions", { method: "POST", body: { ...session.body, exclude: session.asked } });
      if (!again) {
        session.input = { resumeText: session.body.resumeText, jobDescription: data.jobDescription, jobTitle: data.jobTitle, stage: session.stage };
        const label = (INTERVIEW_STAGES.find(([value]) => value === session.stage) || INTERVIEW_STAGES[0])[1];
        list.replaceChildren();
        results.replaceChildren(
          h("p", { class: "muted" }, `${label}${data.jobTitle ? ` · ${data.jobTitle}` : ""}. ${session.stage === "ask" ? "Pick the ones that fit your interview." : "Start with the first one."}`),
          list, moreRow);
      }
      renderQuestions(data.questions || []);
      moreRow.replaceChildren(isFree()
        ? freeUpgrade("Practice every question", "The Free plan includes 1 practice question. Pro and Interview Prep give you unlimited questions for any job, with coaching on every answer.")
        : more);
    } catch (error) {
      const message = error.code === "free_limit_reached"
        ? freeUpgrade("Keep practicing with Jane", error.message)
        : h("div", { class: "form-error" }, error.code === "pro_required" ? "Choose a plan to practice interviews with Jane." : error.message);
      if (again) moreRow.replaceChildren(message, more);
      else results.replaceChildren(message);
    } finally {
      button.disabled = false;
    }
  }

  const go = h("button", { class: "btn btn-primary", type: "submit" }, "Get my practice questions");
  const form = h("form", { class: "card stack", onsubmit: (event) => {
    event.preventDefault();
    loadQuestions(false);
  } },
    h("div", { class: "grid-2" },
      h("label", { class: "field" }, "Job", jobSelect),
      h("label", { class: "field" }, "Résumé", resumeSelect)),
    pasteArea,
    h("label", { class: "field" }, "Interview stage", stageSelect),
    h("div", { class: "row" }, go),
    h("p", { class: "faint small" }, "Tip: practice out loud. Voice answers work in Chrome, Edge and Safari; your microphone is only used while you're answering."));

  const freeNote = isFree()
    ? h("p", { class: "faint small" }, freeLeft() && freeLeft().interviewSetsLeft === 0
      ? "You've used your free practice question. "
      : "Free plan: try 1 practice question, with your pace, filler words and length checked. ",
      link("/pricing", {}, "Unlimited practice is in Pro and Interview Prep"), ".")
    : null;
  return h("div", { class: "stack" }, head, freeNote, form, results);
}

// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Interview prep relaunch (Nov 2026): Jane is an interview prep service.
// Pick the company, the position and the round; Jane writes the questions,
// you pick the ones to practice, answer out loud and get graded. Résumés and
// cover letters help people land the interview. Job search is retired.
// ---------------------------------------------------------------------------

// Interview types, keyed as tailor.InterviewStages: [label, what it covers].
const ROUND_TYPES = {
  phone: ["Phone screen", "A recruiter call: your story, why this company, salary and timing."],
  behavioral: ["Behavioral", "Stories from your résumé, told with clear results."],
  role: ["Technical deep dive", "Explain concepts, debug a scenario and walk through your approach, out loud."],
  design: ["System design", "Design a system at real scale: requirements, sizing and trade-offs."],
  first: ["Hiring manager", "How you work, and what you'd own in your first 90 days."],
  final: ["Final round", "A mix of every round, at the level of an onsite loop."],
  mixed: ["Full loop", "One question from each round, start to finish, like the whole process."],
  exec: ["Executive / bar raiser", "Judgment, hard decisions and long-term thinking, with a senior leader."],
  case: ["Case interview", "Business problems talked through out loud, with numbers to reason with."],
  product: ["Product sense", "Improve a product, design for a user, pick the metrics."],
  situational: ["Situational", "“What would you do if…” questions about real problems in the job."],
  walkthrough: ["Résumé walkthrough", "Every role on your résumé, and deep follow-ups on your biggest projects."],
  panel: ["Panel", "Several interviewers at once, each with their own focus."],
  values: ["Culture and values", "Real examples that match what the company says it values."],
  clinical: ["Clinical scenarios", "Patient situations, safety, priorities and teamwork."],
  teaching: ["Teaching", "Classroom management, lesson planning and a sample lesson."],
  sales: ["Sales role-play", "Discovery, objections, closing and your numbers."],
  service: ["Customer service", "Calming a frustrated customer, step by step."],
  finance: ["Finance", "Concepts and judgment calls, answered out loud."],
  data: ["Data and analytics", "Metrics, A/B tests and explaining an analysis."],
  marketing: ["Marketing", "Campaign ideas, channels, budgets and results."],
  leadership: ["Leadership", "Hiring, feedback, low performers and conflict."],
  change: ["Career change", "Why the switch, and what carries over."],
  salary: ["Salary negotiation", "Answer the offer and ask for more, out loud."],
  ask: ["Questions for them", "Smart questions to ask at the end, and why they work."],
};
const ROUND_GROUPS = [
  ["By stage", ["exec", "panel"]],
  ["By format", ["case", "product", "situational", "walkthrough", "values"]],
  ["For your field", ["clinical", "teaching", "sales", "service", "finance", "data", "marketing", "leadership"]],
  ["Special", ["change", "salary", "ask"]],
];
const roundLabel = (key) => (ROUND_TYPES[key] || ROUND_TYPES.mixed)[0];

// suggestedRounds are the rounds a job usually has, in order.
function suggestedRounds(position) {
  const p = ` ${String(position || "").toLowerCase()} `;
  const has = (re) => re.test(p);
  if (has(/engineer|developer|software|programmer|devops|\bsre\b|architect|full ?stack|backend|frontend/)) return ["phone", "behavioral", "role", "design", "first", "final"];
  if (has(/nurse|\brn\b|\blpn\b|clinical|medical|pharm|therap|health|\bcna\b|physician|dental/)) return ["phone", "behavioral", "clinical", "first", "final"];
  if (has(/teach|educat|tutor|professor|instructor/)) return ["phone", "behavioral", "teaching", "first", "final"];
  if (has(/sales|account executive|\bsdr\b|\bbdr\b|business development/)) return ["phone", "behavioral", "sales", "first", "final"];
  if (has(/customer|support|service|call center|receptionist|front desk/)) return ["phone", "behavioral", "service", "first", "final"];
  if (has(/data|analyst|scientist|analytics/)) return ["phone", "behavioral", "data", "role", "final"];
  if (has(/product manager|\bpm\b|product owner/)) return ["phone", "behavioral", "product", "first", "final"];
  if (has(/consult/)) return ["phone", "behavioral", "case", "final"];
  if (has(/accountant|accounting|finance|financial|bookkeep|\bcpa\b|auditor/)) return ["phone", "behavioral", "finance", "first", "final"];
  if (has(/marketing|brand|social media|content/)) return ["phone", "behavioral", "marketing", "first", "final"];
  if (has(/manager|director|\blead\b|head of|supervisor/)) return ["phone", "behavioral", "leadership", "first", "final"];
  return ["phone", "behavioral", "first", "final"];
}

const SESSIONS_KEY = "janecareers.sessions";
function savedSessions() {
  try { return JSON.parse(localStorage.getItem(SESSIONS_KEY) || "[]"); } catch { return []; }
}
function saveSession(session) {
  try {
    const list = [session, ...savedSessions()].slice(0, 12);
    localStorage.setItem(SESSIONS_KEY, JSON.stringify(list));
  } catch { /* storage is optional */ }
}

const ICON_MIC_DARK = '<svg aria-hidden="true" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/></svg>';
const ICON_ARROW = '<svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M5 12h14M13 6l6 6-6 6"/></svg>';
const ICON_BACK = '<svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M15 18l-6-6 6-6"/></svg>';
const ICON_REFRESH = '<svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7"/></svg>';

// ---------------------------------------------------------------------------
// Homepage
// ---------------------------------------------------------------------------

function ipLabel(text) {
  return h("p", { class: "ip-label" }, text);
}

function ipDemo() {
  const bars = [30, 70, 45, 95, 55, 80, 35, 65, 85, 40, 60, 25, 50, 75];
  const stat = (label, value, note) => h("div", { class: "ip-stat" }, h("span", { class: "ip-mono" }, label), h("b", {}, value), h("span", { class: "ip-good" }, note));
  return h("div", { class: "ip-demo" },
    h("div", { class: "ip-demo-q" },
      h("div", { class: "ip-demo-top" }, h("span", {}, "Apple · Software Engineer · System design"), h("span", {}, "Question 1 of 3")),
      h("p", { class: "ip-demo-title" }, "Design a distributed log pipeline that counts keywords across a fleet."),
      h("div", { class: "ip-listen" }, h("span", { class: "rec-dot" }), h("b", {}, "Listening"), h("span", { class: "ip-muted" }, "1:42"),
        h("div", { class: "ip-wave", "aria-hidden": "true" }, bars.map((v) => h("i", { style: `height:${v}%` })))),
      h("p", { class: "ip-demo-said" }, "“First, scale. 2 million lines a second at about 200 bytes each is 400 megabytes a second. So an agent on each host sends keyword counts instead of raw logs…”")),
    h("div", { class: "ip-demo-side" },
      h("div", { class: "ip-stats" }, stat("Pace", "138", "Good"), stat("Fillers", "5 → 1", "Better"), stat("Length", "3:05", "Just right")),
      h("div", { class: "ip-demo-fb" },
        h("span", { class: "ip-mono" }, "Jane's feedback"),
        h("p", {}, h("span", { class: "ip-ok", "aria-hidden": "true" }, "✓"), "You sized the load before designing."),
        h("p", {}, h("span", { class: "ip-warn", "aria-hidden": "true" }, "!"), "“error” will flood one Kafka partition. Salt hot keywords, then merge."),
        h("p", {}, h("span", { class: "ip-warn", "aria-hidden": "true" }, "!"), "Use a count-min sketch for the daily top keywords."))));
}

function ipCard(num, title, text) {
  return h("div", { class: "ip-card" }, h("span", { class: "ip-num" }, num), h("h3", {}, title), h("p", {}, text));
}

const IP_FAQ = [
  ["Are these the company's real interview questions?", "No. Jane writes the questions this kind of round usually asks at that company, based on the role, your résumé and what the company is known for. She never claims they're leaked or official."],
  ["Do I have to talk out loud?", "That's the point: knowing an answer isn't the same as saying it. Voice answers work in Chrome, Edge and Safari. You can also type an answer the way you'd say it."],
  ["What does Jane grade?", "How you sound (pace, filler words and length) and what you said: what works, what to fix, and a stronger version built from your own answer."],
  ["Can I practice for any job?", "Yes. Software engineers, nurses, teachers, sales, customer service, analysts, managers and more, from the phone screen to the final round."],
  ["Can I cancel?", "Yes, any time from Plan & billing, and your first payment has a 3-day money-back guarantee."],
];

// ipHearSection: an example answer with its filler words marked, to show why
// practicing out loud matters.
function ipHearSection() {
  const fill = (word) => h("mark", { class: "ip-fill" }, word);
  const fillerBars = new Set([6, 7, 27, 28, 45, 46, 62, 63, 80, 81, 96, 97, 103, 104]);
  const wave = h("div", { class: "ip-wave", "aria-hidden": "true" }, Array.from({ length: 110 }, (_, i) => {
    const height = 8 + Math.abs(Math.sin(i / 5)) * 46 * (0.5 + ((i * 37) % 11) / 22);
    return h("span", { class: fillerBars.has(i) ? "on" : "", style: `height:${height.toFixed(0)}px` });
  }));
  const tip = (label, text) => h("div", { class: "ip-hear-tip" }, h("span", { class: "ip-mono" }, label), h("p", {}, text));
  return h("section", { class: "ip-section", id: "out-loud" },
    ipLabel("Why out loud"),
    h("h2", {}, "Hear what the interviewer hears."),
    h("p", { class: "ip-sub" }, "You can't hear filler words when you rehearse in your head. Jane catches every one."),
    h("div", { class: "ip-hear" },
      h("div", { class: "ip-hear-head" },
        h("div", {},
          h("span", { class: "ip-mono" }, "Example · Microsoft · Software Engineer · System design"),
          h("p", { class: "ip-hear-q" }, "Design the message delivery system for a team chat app like Microsoft Teams.")),
        h("div", { class: "ip-hear-chips" }, h("span", { class: "ip-chip warn" }, "7 filler words"), h("span", { class: "ip-chip" }, "164 wpm · a bit fast"))),
      wave,
      h("span", { class: "ip-mono" }, "The answer · 1:48"),
      h("p", { class: "ip-hear-text" },
        "So ", fill("um"), ", I'd start with who's online. Each client keeps a WebSocket open to a gateway, and ", fill("like"),
        ", messages go into a queue partitioned by chat ID, so ", fill("you know"), ", order holds inside each conversation. ",
        fill("Basically"), " every message gets written to storage first, and ", fill("um"),
        ", people who are offline pull what they missed when they reconnect. It has to handle ", fill("like"), " a lot of traffic, ", fill("I guess"), ".")),
    h("div", { class: "ip-grid-3 ip-hear-tips" },
      tip("Most used", "\u201cum\u201d and \u201clike\u201d"),
      tip("Try instead", "Pause for a second. Silence sounds like thinking, not guessing."),
      tip("Say it with numbers", "\u201cPlan for 50,000 messages a second\u201d beats \u201ca lot of traffic, I guess.\u201d")));
}

async function viewLanding() {
  if (state.user === undefined) await loadUser();
  const plansInfo = await loadPlans();
  if (state.user) await loadBilling().catch(() => null);
  const start = state.user ? "/app/interview" : "/signup";
  const cta = (label) => link(start, { class: "ip-btn ip-btn-primary" }, label);
  const item = ([q, a]) => h("details", { class: "ip-faq-item" }, h("summary", {}, q), h("p", {}, a));
  mount(h("div", { class: "ip" },
    siteNav(),
    h("main", {},
      h("section", { class: "ip-hero" },
        h("div", { class: "ip-glow", "aria-hidden": "true" }),
        h("div", { class: "ip-hero-copy" },
          h("span", { class: "ip-pill" }, svgIcon(ICON_MIC_DARK), "Interview prep, out loud"),
          h("h1", {}, "Practice the interview ", h("strong", {}, "before the interview.")),
          h("p", { class: "ip-lead" }, "Pick the company, the job and the round. Practice the questions they're likely to ask, out loud, and Jane grades every answer like an interviewer would."),
          h("div", { class: "ip-ctas" }, cta("Start a free mock interview"), h("a", { class: "ip-btn", href: "#how" }, "See how it works")),
          h("p", { class: "ip-note" }, state.user ? "Welcome back." : "Free to try. No card needed.")),
        ipDemo()),

      ipHearSection(),

      h("section", { class: "ip-section", id: "how" },
        ipLabel("How it works"),
        h("h2", {}, "From nervous to ready, one answer at a time."),
        h("div", { class: "ip-grid-3" },
          ipCard("01", "Set up your interview", "Company, position, round and your résumé. Add the job posting for questions about that exact team."),
          ipCard("02", "Pick your questions", "Jane writes the questions they're likely to ask. Choose the ones you want to practice."),
          ipCard("03", "Answer out loud, get graded", "What you said, how you said it, and a stronger version built from your own answer. Then say it again."))),

      h("section", { class: "ip-section", id: "rounds" },
        ipLabel("Every round"),
        h("h2", {}, "Any job. Any company. Any round."),
        h("p", { class: "ip-sub" }, "From a nurse's phone screen to a software engineer's final loop."),
        h("div", { class: "ip-grid-3" }, ["phone", "behavioral", "role", "design", "first", "final"].map((key) =>
          h("div", { class: "ip-round" }, h("h3", {}, ROUND_TYPES[key][0]), h("p", {}, ROUND_TYPES[key][1])))),
        h("p", { class: "ip-more" }, "…and 20+ more, from case interviews to salary negotiation.")),

      h("section", { class: "ip-section", id: "resume" },
        h("div", { class: "ip-panel" },
          ipLabel("Before the interview"),
          h("h2", {}, "Land the interview with a résumé made for the job."),
          h("p", { class: "ip-sub" }, "Most résumés get skipped because they don't say what the posting asks for. Paste the job, and Jane tailors your résumé and cover letter to stand out."),
          h("div", { class: "ip-grid-3" },
            ipCard("1", "Paste the job posting", "Jane reads what they want: skills, experience, keywords."),
            ipCard("2", "Get your tailored résumé", "Your real experience, rewritten to match. What's missing is flagged, never made up."),
            ipCard("3", "Add a cover letter", "Three short paragraphs. Download both as PDF or Word.")),
          h("div", { class: "ip-compare" },
            h("div", {}, h("span", { class: "ip-mono" }, "Before"), h("p", { class: "ip-muted" }, "Worked on backend services and fixed bugs.")),
            h("div", { class: "ip-after" }, h("span", { class: "ip-mono ip-good" }, "After, for this job"), h("p", {}, "Built Go services and PostgreSQL schemas behind REST APIs used by 200,000 customers."))),
          link(state.user ? "/app/documents" : "/signup", { class: "ip-btn ip-btn-primary" }, "Tailor my résumé"))),

      pricingSection(plansInfo, { full: location.pathname === "/pricing" }),

      h("section", { class: "ip-section ip-faq", id: "faq" },
        ipLabel("Questions"),
        h("h2", {}, "Good to know."),
        h("div", { class: "ip-faq-list" }, IP_FAQ.map(item))),

      h("section", { class: "ip-final" },
        h("div", { class: "ip-glow ip-glow-low", "aria-hidden": "true" }),
        h("h2", {}, "Your next interview, ", h("strong", {}, "rehearsed.")),
        cta("Start a free mock interview"))),
    siteFooter()));
}

function siteNav() {
  const signedIn = !!state.user;
  const header = h("header", { class: "site-nav" });
  const toggle = h("button", { type: "button", class: "nav-toggle", "aria-label": "Menu", "aria-expanded": "false", onclick: () => {
    const open = !header.classList.contains("menu-open");
    header.classList.toggle("menu-open", open);
    document.body.classList.toggle("nav-locked", open);
    toggle.setAttribute("aria-expanded", open ? "true" : "false");
  } }, svgIcon('<svg aria-hidden="true" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M4 7h16M4 12h16M4 17h16"/></svg>'));
  const plain = (href, label) => h("a", { class: "nav-trigger nav-plain", href }, label);
  header.append(h("div", { class: "container nav-bar" },
    brand(),
    h("div", { class: "nav-panel" },
      h("nav", { class: "nav-menus", "aria-label": "Main" },
        plain("/#how", "How it works"),
        plain("/#rounds", "Rounds"),
        plain("/#resume", "Résumé & cover letter"),
        link("/pricing", { class: "nav-trigger nav-plain" }, "Pricing"),
        plain("/interview-questions", "Guides")),
      h("div", { class: "nav-actions" },
        signedIn
          ? link("/app", { class: "btn btn-primary btn-sm" }, "My account")
          : [link("/login", { class: "nav-signin" }, "Sign in"), link("/signup", { class: "btn btn-primary btn-sm" }, "Start free")]),
    ),
    toggle,
  ));
  return header;
}

// ---------------------------------------------------------------------------
// Signed in: dashboard
// ---------------------------------------------------------------------------

function usageMeter(usage) {
  if (!usage || !usage.answersLimit) return null;
  const used = Math.min(usage.answersUsed, usage.answersLimit);
  const unlimited = usage.answersLimit >= 300;
  return h("div", { class: "card ip-usage stack-sm" },
    h("div", { class: "row-between" },
      h("span", { class: "ip-mono" }, usage.period === "month" ? "This month" : "Free plan"),
      unlimited ? h("span", { class: "pill" }, "Unlimited") : null),
    h("b", {}, unlimited ? `${usage.answersUsed} graded answers` : `${used} of ${usage.answersLimit} graded answers used`),
    unlimited ? null : h("div", { class: "ip-bar", "aria-hidden": "true" }, h("i", { style: `width:${Math.round(100 * used / usage.answersLimit)}%` })),
    unlimited ? null : link("/pricing", { class: "small" }, usage.period === "month" ? "Go unlimited with Pro" : "See plans"));
}

async function viewDashboard() {
  const resumes = await loadResumes().catch(() => []);
  const sessions = savedSessions();
  const usage = state.billing && state.billing.usage;
  const newButton = link("/app/interview", { class: "btn btn-primary" }, svgIcon(ICON_MIC_DARK), "New mock interview");
  const again = (s) => {
    state.practicePrefill = { company: s.company, position: s.position, round: s.round };
    navigate("/app/interview");
  };
  return h("div", { class: "stack ip-app" },
    pageHead("Your interview prep", "Pick a company, a job and a round, then practice out loud.", newButton),
    !(resumes || []).length
      ? h("div", { class: "card ip-callout stack-sm" },
        h("h3", {}, "Add your résumé first"),
        h("p", { class: "muted" }, "Jane uses it to write questions about your real work and to grade your answers."),
        h("div", { class: "row" }, link("/app/resumes", { class: "btn btn-primary" }, "Upload résumé")))
      : null,
    h("div", { class: "ip-dash-grid" },
      h("div", { class: "card stack-sm ip-start" },
        h("span", { class: "ip-mono" }, "Start practicing"),
        h("h3", {}, "Got an interview coming up?"),
        h("p", { class: "muted" }, "Tell Jane the company, the position and the round. She writes the questions; you pick the ones to practice."),
        h("div", { class: "row" }, link("/app/interview", { class: "btn btn-primary" }, "Set up a mock interview"))),
      usageMeter(usage)),
    h("div", { class: "card stack-sm" },
      h("h2", { class: "ip-h3" }, "Recent sessions"),
      sessions.length
        ? h("ul", { class: "ip-sessions" }, sessions.slice(0, 6).map((s) => h("li", {},
          h("div", {}, h("b", {}, `${roundLabel(s.round)} · ${s.company || "Any company"}`),
            h("div", { class: "faint small" }, `${s.position || ""} · ${formatDate(s.date)} · ${s.count} question${s.count === 1 ? "" : "s"}`)),
          h("button", { class: "btn btn-sm", type: "button", onclick: () => again(s) }, "Practice again"))))
        : h("p", { class: "muted" }, "Your mock interviews will show here.")),
    h("div", { class: "ip-grid-2" },
      link("/app/documents", { class: "card ip-tool" }, h("b", {}, "Tailor my résumé"), h("span", { class: "muted small" }, "Paste a job posting. Jane tailors your résumé to stand out.")),
      link("/app/documents#letter", { class: "card ip-tool" }, h("b", {}, "Write a cover letter"), h("span", { class: "muted small" }, "Three short paragraphs from your real experience."))));
}

// ---------------------------------------------------------------------------
// Signed in: mock interview (set up, pick questions, practice)
// ---------------------------------------------------------------------------

async function viewMockInterview() {
  const resumes = await loadResumes().catch(() => []);
  const wrap = h("div", { class: "stack ip-app" });
  if (!(resumes || []).length) {
    wrap.append(pageHead("New mock interview", ""), h("div", { class: "card empty" },
      h("h3", {}, "Add your résumé first"),
      h("p", {}, "Jane uses it to write questions about your real work and to grade your answers."),
      link("/app/resumes", { class: "btn btn-primary" }, "Upload résumé")));
    return wrap;
  }
  const prefill = state.practicePrefill || {};
  state.practicePrefill = null;
  const setup = { company: prefill.company || "", position: prefill.position || "", round: prefill.round || "", resumeId: (resumes.find((r) => r.isPrimary) || resumes[0]).id, posting: "" };
  showSetup();
  return wrap;

  function showSetup() {
    const company = h("input", { type: "text", maxlength: 120, placeholder: "e.g. Apple", value: setup.company, autocomplete: "organization" });
    const position = h("input", { type: "text", maxlength: 120, placeholder: "e.g. Software Engineer", value: setup.position });
    const posting = h("textarea", { rows: 4, maxlength: 30000, placeholder: "Paste the job description here" });
    posting.value = setup.posting;
    const resumeSelect = h("select", { "aria-label": "Résumé" }, resumes.map((r) => h("option", { value: r.id }, r.filename)));
    resumeSelect.value = setup.resumeId;

    const chips = h("div", { class: "ip-chips" });
    const note = h("p", { class: "faint small" });
    const hint = h("span", { class: "faint" });
    const morePanel = h("div", { class: "ip-more-panel hidden" });
    const search = h("input", { type: "search", placeholder: "Search: case, negotiation, clinical…", "aria-label": "Search interview types" });
    const groups = h("div", { class: "stack-sm" });
    const moreToggle = h("button", { type: "button", class: "ip-linkbtn", "aria-expanded": "false" }, "More interview types (20+)");

    const pick = (key) => { setup.round = key; paint(); };
    const chip = (key, small) => h("button", { type: "button", class: `ip-chip${small ? " ip-chip-sm" : ""}${setup.round === key ? " on" : ""}`, "aria-pressed": setup.round === key ? "true" : "false", onclick: () => pick(key) }, roundLabel(key));
    function paint() {
      const suggested = suggestedRounds(position.value);
      if (!setup.round) setup.round = suggested.includes("design") ? "design" : suggested[1] || suggested[0];
      chips.replaceChildren(...suggested.map((key) => chip(key)));
      hint.textContent = position.value.trim() ? `Usual rounds for ${position.value.trim()}, in order` : "Usual rounds, in order";
      note.textContent = (ROUND_TYPES[setup.round] || ROUND_TYPES.mixed)[1];
      const q = search.value.trim().toLowerCase();
      groups.replaceChildren(...ROUND_GROUPS.map(([title, keys]) => {
        const shown = keys.filter((key) => !q || `${ROUND_TYPES[key][0]} ${ROUND_TYPES[key][1]}`.toLowerCase().includes(q));
        return shown.length ? h("div", { class: "stack-sm" }, h("span", { class: "ip-mono" }, title), h("div", { class: "ip-chips" }, shown.map((key) => chip(key, true)))) : null;
      }));
      summaryRound.textContent = setup.round === "mixed" ? "Full loop" : `${roundLabel(setup.round)} round`;
    }
    moreToggle.addEventListener("click", () => {
      const open = morePanel.classList.toggle("hidden") === false;
      moreToggle.setAttribute("aria-expanded", open ? "true" : "false");
      moreToggle.textContent = open ? "Hide other interview types" : "More interview types (20+)";
      if (open) search.focus();
    });
    search.addEventListener("input", paint);
    let timer = null;
    position.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => { setup.round = ""; paint(); summaryJob(); }, 350); });
    company.addEventListener("input", () => summaryJob());
    morePanel.append(h("label", { class: "ip-search" }, svgIcon(HD_SEARCH), search), groups);

    const summaryTitle = h("b", {});
    const summaryRound = h("span", { class: "muted" });
    const summaryJob = () => { summaryTitle.textContent = [company.value.trim(), position.value.trim()].filter(Boolean).join(" · ") || "Your interview"; };
    summaryJob();

    const error = h("div", { class: "form-error", hidden: true });
    const go = h("button", { class: "btn btn-primary btn-lg ip-wide", type: "submit" }, "Generate my questions", svgIcon(ICON_ARROW));
    const usage = state.billing && state.billing.usage;
    const freeNote = usage && usage.period === "total"
      ? h("p", { class: "faint small" }, `Free plan: ${Math.max(usage.answersLimit - usage.answersUsed, 0)} of ${usage.answersLimit} graded answers left. `, link("/pricing", {}, "See plans"), ".")
      : null;

    const form = h("form", { class: "card stack ip-form", onsubmit: async (event) => {
      event.preventDefault();
      setup.company = company.value.trim();
      setup.position = position.value.trim();
      setup.posting = posting.value.trim();
      setup.resumeId = resumeSelect.value;
      error.hidden = true;
      if (!setup.position) { error.hidden = false; error.textContent = "Tell Jane the position you're interviewing for."; position.focus(); return; }
      if (setup.posting && setup.posting.length < 80) { error.hidden = false; error.textContent = "That job posting looks too short. Paste the full posting, or leave it empty."; return; }
      go.disabled = true;
      try {
        await generate([]);
      } catch (e) {
        go.disabled = false;
        error.hidden = false;
        error.replaceChildren(e.code === "free_limit_reached" || e.code === "plan_limit_reached" ? freeUpgrade("Keep practicing with Jane", e.message) : e.message);
      }
    } },
      h("div", { class: "grid-2" },
        h("label", { class: "field" }, "Company", company),
        h("label", { class: "field" }, "Position", position)),
      h("fieldset", { class: "ip-fieldset" },
        h("legend", {}, "Which round? ", hint),
        chips, note,
        h("div", { class: "row" }, moreToggle,
          h("button", { type: "button", class: "ip-linkbtn", onclick: () => pick("mixed") }, "Not sure? Practice the full loop")),
        morePanel),
      h("label", { class: "field" }, "Résumé", resumeSelect),
      h("label", { class: "field" }, h("span", {}, "Job posting ", h("span", { class: "faint" }, "Optional. Paste it for questions about this exact team.")), posting),
      error, go, freeNote);

    wrap.replaceChildren(
      pageHead("New mock interview", "Tell Jane the company and the job. She suggests the rounds that job usually has."),
      h("div", { class: "ip-setup" }, form,
        h("aside", { class: "card stack-sm ip-aside" },
          h("span", { class: "ip-mono" }, "Your session"),
          summaryTitle, summaryRound,
          h("ul", { class: "ip-steps" },
            h("li", {}, "Jane writes 8 questions; you pick which to practice"),
            h("li", {}, "Answer out loud, like the real thing"),
            h("li", {}, "Graded on what you said and how you said it")),
          h("p", { class: "faint small" }, "Likely questions for this kind of round, written for the role. Jane never claims they're the company's real questions."))));
    paint();
  }

  async function generate(exclude) {
    wrap.replaceChildren(pageHead("New mock interview", ""), h("div", { class: "card" }, spinnerRow("Jane is writing your questions… (about 20 seconds)")));
    const resume = await resumeText(setup.resumeId).catch(() => "");
    try {
      const data = await api("/api/v1/interview-questions", { method: "POST", body: {
        resumeText: resume, jobTitle: setup.position, company: setup.company,
        jobDescription: setup.posting, stage: setup.round === "mixed" ? "mixed" : setup.round, exclude } });
      loadBilling().then(refreshUsage).catch(() => null);
      setup.input = { resumeText: resume, jobDescription: data.jobDescription || setup.posting, jobTitle: data.jobTitle || setup.position, stage: setup.round };
      showPicker(data.questions || [], exclude);
    } catch (e) {
      showSetup();
      throw e;
    }
  }

  function crumb() {
    return [setup.company, setup.position, setup.round === "mixed" ? "Full loop" : `${roundLabel(setup.round)} round`].filter(Boolean).join(" · ");
  }

  function showPicker(questions, excluded) {
    const picked = new Set(questions.slice(0, 3).map((_, i) => i));
    const list = h("ul", { class: "ip-qlist" });
    const count = h("b", {});
    const start = h("button", { class: "btn btn-primary btn-lg", type: "button" });
    const own = h("input", { type: "text", maxlength: 300, placeholder: "e.g. a question a friend was asked there" });
    const paint = () => {
      list.replaceChildren(...questions.map((q, i) => {
        const box = h("input", { type: "checkbox" });
        box.checked = picked.has(i);
        box.addEventListener("change", () => { if (box.checked) picked.add(i); else picked.delete(i); paint(); });
        return h("li", {}, h("label", { class: `ip-qrow${picked.has(i) ? " on" : ""}` }, box,
          h("span", { class: "stack-sm" },
            h("span", { class: "ip-qtext" }, q.question),
            h("span", { class: "ip-tags" },
              q.level ? h("span", { class: `ip-tag ip-${String(q.level).toLowerCase()}` }, q.level) : null,
              q.tests ? h("span", { class: "ip-tag" }, `Tests: ${q.tests}`) : null,
              q.fromResume ? h("span", { class: "ip-tag ip-resume" }, "From your résumé") : null))));
      }));
      count.textContent = String(picked.size);
      start.textContent = setup.round === "ask" ? `See ${picked.size} questions` : `Start with ${picked.size} question${picked.size === 1 ? "" : "s"}`;
      start.disabled = picked.size === 0;
    };
    start.addEventListener("click", () => {
      const chosen = questions.filter((_, i) => picked.has(i));
      saveSession({ company: setup.company, position: setup.position, round: setup.round, date: new Date().toISOString(), count: chosen.length });
      showPractice(chosen, 0);
    });
    const addOwn = () => {
      const text = own.value.trim();
      if (text.length < 8) return;
      questions.push({ question: text, approach: "Your own question. Answer it the way you would in the room.", level: "", tests: "", fromResume: false });
      picked.add(questions.length - 1);
      own.value = "";
      paint();
    };
    own.addEventListener("keydown", (event) => { if (event.key === "Enter") { event.preventDefault(); addOwn(); } });
    const fresh = h("button", { class: "btn", type: "button", onclick: async () => {
      fresh.disabled = true;
      try { await generate([...excluded, ...questions.map((q) => q.question)].slice(-60)); }
      catch (e) { toast(e.message, true); }
    } }, svgIcon(ICON_REFRESH), "New questions");
    wrap.replaceChildren(
      h("button", { class: "ip-back", type: "button", onclick: showSetup }, svgIcon(ICON_BACK), "Change setup"),
      pageHead("Pick your questions", `${crumb()}. Jane wrote these using your résumé. Pick the ones you want to practice.`, fresh),
      h("div", { class: "row-between ip-pickbar" },
        h("span", { class: "muted small" }, count, " selected"),
        h("span", { class: "row" },
          h("button", { class: "ip-linkbtn", type: "button", onclick: () => { questions.forEach((_, i) => picked.add(i)); paint(); } }, "Select all"),
          h("button", { class: "ip-linkbtn", type: "button", onclick: () => { picked.clear(); paint(); } }, "Clear"))),
      list,
      h("div", { class: "card ip-own" },
        h("label", { class: "field" }, "Add your own question", own),
        h("button", { class: "btn", type: "button", onclick: addOwn }, "Add")),
      h("div", { class: "row-between ip-pickbar" },
        h("p", { class: "faint small" }, "Likely questions for this kind of round, not leaked questions. Hard ones are marked so you can choose your challenge."),
        start));
    paint();
  }

  function showPractice(chosen, index) {
    if (setup.round === "ask") {
      wrap.replaceChildren(
        h("button", { class: "ip-back", type: "button", onclick: () => showPicker(chosen, []) }, svgIcon(ICON_BACK), "Change questions"),
        pageHead("Questions to ask them", crumb()),
        h("div", { class: "stack" }, chosen.map((q, i) => h("div", { class: "card stack-sm" },
          h("div", { class: "row" }, h("span", { class: "step-num" }, String(i + 1)), h("strong", {}, q.question)),
          h("p", { class: "muted small" }, h("strong", {}, "Why ask it: "), q.approach)))));
      return;
    }
    const q = chosen[index];
    const last = index === chosen.length - 1;
    wrap.replaceChildren(
      h("div", { class: "row-between ip-pracbar" },
        h("button", { class: "ip-back", type: "button", onclick: () => showPicker(chosen, []) }, svgIcon(ICON_BACK), "Change questions"),
        h("span", { class: "muted small" }, crumb()),
        h("span", { class: "ip-mono" }, `Question ${index + 1} of ${chosen.length}`)),
      h("div", { class: "ip-progress", "aria-hidden": "true" }, h("i", { style: `width:${Math.round(100 * (index + 1) / chosen.length)}%` })),
      h("article", { class: "card stack-sm ip-question" },
        h("span", { class: "ip-mono" }, roundLabel(setup.round)),
        h("h1", {}, q.question),
        q.approach ? h("details", {}, h("summary", {}, "What a strong answer covers"), h("p", { class: "muted" }, q.approach)) : null),
      practiceBox(q.question, setup.input, { open: true }),
      h("div", { class: "row-between" },
        index > 0 ? h("button", { class: "btn", type: "button", onclick: () => showPractice(chosen, index - 1) }, "Previous question") : h("span", {}),
        last
          ? h("button", { class: "btn btn-primary", type: "button", onclick: () => showDone(chosen) }, "Finish session")
          : h("button", { class: "btn btn-primary", type: "button", onclick: () => showPractice(chosen, index + 1) }, "Next question", svgIcon(ICON_ARROW))));
    window.scrollTo(0, 0);
  }

  function showDone(chosen) {
    wrap.replaceChildren(
      pageHead("Session done", `${crumb()} · ${chosen.length} question${chosen.length === 1 ? "" : "s"}`),
      h("div", { class: "card stack" },
        h("p", {}, "Nice work. The best way to get ready is to say it again: pick another round, or redo the questions that felt shaky."),
        h("ul", { class: "ip-steps" }, chosen.map((q) => h("li", {}, q.question))),
        h("div", { class: "row" },
          h("button", { class: "btn btn-primary", type: "button", onclick: () => showPractice(chosen, 0) }, "Practice these again"),
          h("button", { class: "btn", type: "button", onclick: () => { setup.round = ""; showSetup(); } }, "Practice another round"),
          link("/app", { class: "btn btn-ghost" }, "Back to dashboard"))));
  }
}

// ---------------------------------------------------------------------------
// Signed in: résumé and cover letter for a job posting
// ---------------------------------------------------------------------------

async function viewDocuments() {
  const resumes = await loadResumes().catch(() => []);
  const head = pageHead("Résumé & cover letter", "Paste a job posting. Jane tailors your résumé and writes a cover letter to stand out, from your real experience.");
  if (!(resumes || []).length) {
    return h("div", { class: "stack" }, head, h("div", { class: "card empty" },
      h("h3", {}, "Add your résumé first"),
      h("p", {}, "Jane tailors your real experience; she never makes anything up."),
      link("/app/resumes", { class: "btn btn-primary" }, "Upload résumé")));
  }
  const company = h("input", { type: "text", maxlength: 120, placeholder: "e.g. Apple" });
  const title = h("input", { type: "text", maxlength: 160, placeholder: "e.g. Software Engineer" });
  const posting = h("textarea", { rows: 8, maxlength: 30000, placeholder: "Paste the full job posting" });
  const resumeSelect = h("select", { "aria-label": "Résumé" }, resumes.map((r) => h("option", { value: r.id }, r.filename)));
  resumeSelect.value = (resumes.find((r) => r.isPrimary) || resumes[0]).id;
  const getInput = async () => {
    if (posting.value.trim().length < 80) throw new Error("Paste the full job posting first, so Jane knows what they want.");
    return { resumeText: await resumeText(resumeSelect.value), jobDescription: posting.value.trim(), jobTitle: title.value.trim(), company: company.value.trim() };
  };
  const usage = state.billing && state.billing.usage;
  const left = usage && usage.period === "month" && usage.resumesLimit < 300
    ? h("p", { class: "faint small" }, `This month: ${usage.resumesUsed} of ${usage.resumesLimit} tailored résumés and ${usage.lettersUsed} of ${usage.lettersLimit} cover letters used.`)
    : null;
  const letter = coverLetterSection(getInput);
  letter.id = "letter";
  return h("div", { class: "stack ip-app" }, head,
    h("div", { class: "card stack" },
      h("div", { class: "grid-2" }, h("label", { class: "field" }, "Company", company), h("label", { class: "field" }, "Job title", title)),
      h("label", { class: "field" }, "Job posting", posting),
      h("label", { class: "field" }, "Résumé", resumeSelect),
      left),
    tailoredResumeSection(getInput),
    letter);
}

render();
