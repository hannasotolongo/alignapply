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
  "/fit-check": viewFitCheckPublic,
  "/login": () => viewAuth("login"),
  "/signup": () => viewAuth("signup"),
  "/forgot-password": viewForgotPassword,
  "/reset-password": viewResetPassword,
  "/app": () => appPage("home", viewHome),
  "/app/home": () => navigate("/app", { replace: true }),
  "/app/welcome": viewOnboarding,
  "/app/activate": viewActivate,
  "/app/fit-check": () => appPage("fit", viewFitCheckApp),
  "/app/jobs": () => appPage("jobs", viewJobs),
  "/app/resumes": () => appPage("resumes", viewResumes),
  "/app/profile": () => appPage("profile", viewProfile),
  "/app/applications": () => appPage("applications", viewApplications),
  "/app/billing": () => appPage("billing", viewBilling),
};

function navigate(path, { replace } = {}) {
  if (replace) history.replaceState(null, "", path);
  else history.pushState(null, "", path);
  render();
}

window.addEventListener("popstate", render);

async function render() {
  const path = location.pathname.replace(/\/+$/, "") || "/";
  const view = routes[path];
  if (!view) {
    mount(viewNotFound());
    return;
  }
  await view();
  if (path === "/pricing") {
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

function siteNav() {
  const signedIn = !!state.user;
  return h("header", { class: "site-nav" },
    h("div", { class: "container" },
      brand(),
      h("nav", { class: "row" },
        link("/pricing", { class: "btn btn-ghost btn-sm" }, "Pricing"),
        signedIn
          ? link("/app", { class: "btn btn-primary btn-sm" }, "My account")
          : [
              link("/login", { class: "btn btn-ghost btn-sm" }, "Sign in"),
              link("/signup", { class: "btn btn-primary btn-sm" }, "Get started"),
            ],
      ),
    ),
  );
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
  return h("footer", { class: "site-footer" },
    h("div", { class: "container row-between" },
      h("span", {}, `© ${new Date().getFullYear()} ${BRAND} · Payments secured by Stripe · Customer service: `, h("a", { href: "tel:+16468212027" }, "646-821-2027")),
      legalLinks("footer-links"),
    ),
  );
}

// exampleResultCard previews Jane's matches screen with clearly labeled
// sample data.
function exampleResultCard() {
  const rows = [
    ["ICU Registered Nurse", "Baptist Health · Miami, FL", 92, "good", "Apply. You meet every must-have."],
    ["RN, Cardiac Step-Down", "Jackson Health · Miami, FL", 88, "good", "Strong fit. One nice-to-have to add."],
    ["Charge Nurse, Emergency", "Mount Sinai · Miami Beach", 71, "mid", "Close one gap first: 3+ years ED."],
  ];
  return h("div", { class: "card demo-card stack-sm" },
    h("div", { class: "row-between" },
      h("div", { class: "stack-sm demo-head" },
        h("span", { class: "demo-title" }, "Your matches today"),
        h("span", { class: "faint small" }, "Registered Nurse · Miami, FL")),
      h("span", { class: "pill" }, "Example")),
    rows.map(([title, meta, pct, cls, note]) => {
      const ring = h("div", { class: `score-ring mini ${cls}` }, h("span", {}, `${pct}%`));
      ring.style.setProperty("--pct", String(pct));
      return h("div", { class: "demo-match" },
        h("div", { class: "stack-sm demo-match-text" },
          h("strong", {}, title),
          h("span", { class: "faint small" }, meta),
          h("span", { class: `small demo-note demo-${cls}` }, note)),
        ring);
    }),
    h("div", { class: "demo-foot" },
      h("span", { class: "truth-badge" }, "✓ Truth-locked résumé ready for each"),
      h("span", { class: "faint small" }, "Scored against your résumé, automatically")),
  );
}

async function viewLanding() {
  if (state.user === undefined) await loadUser();
  const plansInfo = await loadPlans();
  const signedIn = !!state.user;

  const features = [
    ["%", "Automatic match scores", "Know your odds before you apply.", "Jane scores every job she finds against your résumé, automatically, and tells you straight: apply, close a gap first, or skip it."],
    ["↗", "Gap Closer", "Close the gaps that cost you interviews.", "Every missing requirement, sorted: things you have but didn't list, quick certifications, and real gaps worth knowing about."],
    ["✓", "Truth-locked Application Kit", "A résumé tailored to every job. Every word true.", "Jane rewrites your summary and bullets for the job, adds the right keywords, and preps you for the interview, using only what's on your résumé."],
    ["⇄", "Best-résumé pick", "Got more than one résumé? Jane picks the winner.", "Upload every version you have. For each job, Jane scores them all and tells you which one to send."],
    ["⌕", "Jobs found for you", "Your best matches, waiting when you sign in.", "Tell Jane the job title and city once. She finds open jobs, ranks them best fit first, and links you straight to the employer's real application page."],
    ["▤", "Application tracker", "Every application in one place.", "Save jobs, mark them applied, and move them through interview and offer, so you always know where you stand and who to follow up with."],
  ];

  const compare = [
    ["Shows you every job", "Tells you which jobs you can actually get"],
    ["A vague match score", "Every requirement checked against your résumé"],
    ["Silent about what you're missing", "A plan for every gap"],
    ["AI résumé writers that invent skills and numbers", "Truth-locked: every line traced to your real résumé"],
    ["“Easy apply” into a black hole", "Links to the employer's real application page"],
  ];

  const faqs = [
    ["What does Jane actually do?", "She reads a job posting the way a recruiter does, checks each requirement against your résumé, tells you whether to apply, shows you how to close the gaps, and tailors your résumé for that job. Then she tracks the application for you."],
    ["What does “truth-locked” mean?", "When Jane tailors your résumé, every bullet has to come from a line you actually wrote, and she can't add numbers, skills or credentials you don't have. If she can't trace a line back to your résumé, she throws it out. Recruiters can tell when a résumé was padded, and Jane won't do that to you."],
    ["Why do I need a card for the free trial?", "It keeps the trial to one per person. You won't be charged during the 3 days, and you can cancel any time from Plan & billing on the website."],
    ["Is my résumé private?", "Your PDF is read right in your browser. Only the text is stored, it's never shared with employers, and you can delete it, or your whole account, at any time."],
    ["Does Jane work for nurses and healthcare jobs?", "Yes. Jane knows an RN license from a BLS certification, matches “RN” to “Registered Nurse”, and never counts a credential you don't hold."],
    ["How do I reach a person?", "Call customer service at 646-821-2027."],
  ];

  mount(h("div", {},
    siteNav(),
    h("main", {},
      h("section", { class: "hero hero-split" },
        h("div", { class: "container hero-grid" },
          h("div", { class: "hero-copy" },
            h("span", { class: "pill eyebrow" }, "Meet Jane, your job-search copilot"),
            h("h1", {}, "Know if you'll get the interview ", h("span", { class: "hl" }, "before"), " you apply."),
            h("p", { class: "lead" }, "Know your match before you apply. Jane finds the jobs you fit, shows what's missing, tailors your strongest résumé, and gets you ready for the interview. Every application, tracked. No more applying blind."),
            h("div", { class: "row" },
              link(signedIn ? "/app" : "/signup", { class: "btn btn-primary btn-lg" }, signedIn ? "See my matches" : "See my matches free"),
              signedIn ? null : h("a", { class: "btn btn-lg", href: "#fit-check" }, "Try it on one job"),
            ),
            h("p", { class: "faint small hero-note" }, "3 days free · Cancel anytime · Jane does the searching for you"),
          ),
          exampleResultCard(),
        ),
      ),
      h("section", { class: "section", id: "features" },
        h("div", { class: "container" },
          h("h2", { class: "section-title" }, "Everything Jane does for you"),
          h("p", { class: "section-sub" }, "Less searching, less guessing, more interviews. Here's how Jane gets you there."),
          h("div", { class: "grid-3 features-grid" },
            features.map(([icon, name, promise, body]) =>
              h("div", { class: "card feature-card" },
                h("div", { class: "row" },
                  h("div", { class: "step-num", "aria-hidden": "true" }, icon),
                  h("span", { class: "section-label" }, name)),
                h("h3", {}, promise),
                h("p", { class: "muted" }, body),
              )),
          ),
          h("div", { class: "row center section-gap" },
            link(signedIn ? "/app" : "/signup", { class: "btn btn-primary btn-lg" }, signedIn ? "Go to my account" : "Get all of it free for 3 days")),
        ),
      ),
      signedIn ? null : h("section", { class: "section section-alt", id: "fit-check" },
        h("div", { class: "container narrow stack" },
          h("div", { class: "stack-sm center" },
            h("span", { class: "pill eyebrow" }, "Free demo"),
            h("h2", { class: "section-title" }, "Want to see it first? Try one job free"),
            h("p", { class: "section-sub" }, "Upload your résumé and paste a job posting. You'll get your score and Jane's verdict free. The gap plan and tailored résumé come with your trial."),
          ),
          fitCheckTool("public"),
        ),
      ),
      h("section", { class: "section" },
        h("div", { class: "container truth-grid" },
          h("div", { class: "stack-sm" },
            h("span", { class: "truth-badge" }, "✓ Truth-locked"),
            h("h2", { class: "section-title left" }, "The résumé AI that can't lie for you."),
            h("p", { class: "muted" }, "Most AI résumé tools pad your résumé with skills and numbers you never had, and recruiters can tell. Jane tailors your résumé to each job using only what you actually wrote. Every bullet is traced back to a line on your résumé, and anything she can't trace gets thrown out."),
            h("p", { class: "muted" }, "You walk into the interview able to back up every word."),
          ),
          h("div", { class: "card inset stack-sm" },
            h("span", { class: "section-label" }, "How a tailored line works"),
            h("p", { class: "faint small" }, "Your résumé says:"),
            h("p", {}, "“Educated patients and families on discharge plans”"),
            h("p", { class: "faint small" }, "Jane writes for a job asking for patient education:"),
            h("p", { class: "text-good" }, "✓ “Delivered patient and family education on discharge plans”"),
            h("p", { class: "faint small" }, "Jane will never write:"),
            h("p", { class: "text-error" }, "✕ “Cut readmissions 20% through discharge teaching”, a number you never wrote"),
          ),
        ),
      ),
      h("section", { class: "section" },
        h("div", { class: "container narrow" },
          h("h2", { class: "section-title" }, "Why not just use a job board?"),
          h("div", { class: "card compare" },
            h("div", { class: "compare-row compare-head" }, h("span", {}, "Job boards & AI résumé tools"), h("span", {}, BRAND)),
            compare.map(([them, us]) => h("div", { class: "compare-row" },
              h("span", { class: "faint" }, them), h("span", {}, h("span", { class: "text-good", "aria-hidden": "true" }, "✓ "), us))),
          ),
        ),
      ),
      pricingSection(plansInfo),
      h("section", { class: "section" },
        h("div", { class: "container narrow stack" },
          h("h2", { class: "section-title" }, "Questions"),
          h("div", { class: "faq" }, faqs.map(([q, a]) => h("details", { class: "card faq-item" }, h("summary", {}, q), h("p", { class: "muted" }, a)))),
        ),
      ),
      h("section", { class: "section cta-band" },
        h("div", { class: "container center stack-sm" },
          h("h2", { class: "section-title" }, "Stop applying blind."),
          h("p", { class: "section-sub" }, "Let Jane show you where you fit, and help you land it."),
          h("div", { class: "row center" },
            link(signedIn ? "/app" : "/signup", { class: "btn btn-primary btn-lg" }, signedIn ? "See my matches" : "See my matches free")),
        ),
      ),
    ),
    siteFooter(),
  ));
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
  return plan.trialDays && trialAvailable() ? `Start ${plan.trialDays}-day free trial` : `Subscribe for ${plan.displayPrice}/${plan.interval}`;
}

function checkoutNote() {
  const monthly = planFor("monthly");
  const sprint = planFor("sprint");
  return h("p", { class: "legal-note" },
    monthly && monthly.trialDays
      ? `Pro Monthly is free for ${monthly.trialDays} days, then ${monthly.displayPrice}/month until you cancel. Cancel before the trial ends and you won't be charged. `
      : "Subscriptions renew automatically until you cancel. ",
    sprint ? `The ${sprint.name} is a one-time ${sprint.displayPrice} payment for ${sprint.interval} and doesn't renew. ` : "",
    "7-day money-back guarantee on your first payment. See our ",
    h("a", { href: "/terms" }, "Terms"), " and ", h("a", { href: "/refunds" }, "Refund Policy"), ".");
}

function planCards(plansInfo, { compact = false } = {}) {
  const enabled = plansInfo.billingEnabled;
  const monthly = planFor("monthly") || { key: "monthly", name: "Pro Monthly", displayPrice: "$14.99", interval: "month", trialDays: 3 };
  const sprint = planFor("sprint") || { key: "sprint", name: "Job Search Sprint", displayPrice: "$39", interval: "90 days" };
  const school = planFor("school") || { key: "school", name: "School License", displayPrice: "$1,000", interval: "year" };

  const features = [
    "Unlimited Fit Checks and job-match searches",
    "Gap Closer for every job",
    "Best-résumé pick for each application",
    plansInfo.gmailEnabled ? "Application tracker with Gmail sync" : "Application tracker, from saved to offer",
  ];
  const ticks = () => h("ul", { class: "ticks" }, features.map((f) => h("li", {}, f)));

  const cards = [
    h("div", { class: "card price-card" },
      h("div", {}, h("h3", {}, monthly.name), h("p", { class: "muted small" }, monthly.trialDays ? `${monthly.trialDays}-day free trial, then monthly` : "Billed monthly")),
      h("div", { class: "price" }, monthly.displayPrice, " ", h("small", {}, `/ ${monthly.interval}`)),
      compact ? null : ticks(),
      h("button", { class: "btn btn-primary btn-block", disabled: !enabled, onclick: () => startCheckout("monthly") },
        enabled ? subscribeLabel(monthly) : "Coming soon"),
    ),
    h("div", { class: "card price-card featured" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, sprint.name), h("p", { class: "muted small" }, `One payment, ${sprint.interval} of Pro`)),
        h("span", { class: "pill pill-pro" }, "Best value"),
      ),
      h("div", { class: "price" }, sprint.displayPrice, " ", h("small", {}, "one-time")),
      compact ? null : ticks(),
      h("button", { class: "btn btn-primary btn-block", disabled: !enabled, onclick: () => startCheckout("sprint") },
        enabled ? `Get the Sprint` : "Coming soon"),
    ),
  ];

  if (!compact) {
    cards.push(h("div", { class: "card price-card" },
      h("div", {}, h("h3", {}, "Schools & programs"), h("p", { class: "muted small" }, "Career centers, nursing programs, bootcamps")),
      h("div", { class: "price" }, school.displayPrice, " ", h("small", {}, `/ ${school.interval}`)),
      h("ul", { class: "ticks" },
        h("li", {}, "Pro for every student with a verified school email"),
        h("li", {}, "No seat counting or codes to hand out"),
        h("li", {}, "Free 2-week pilot available"),
      ),
      state.user
        ? link("/app/billing", { class: "btn btn-block" }, "Get a school license")
        : link("/signup", { class: "btn btn-block" }, "Get a school license"),
    ));
  }

  return cards;
}

function pricingSection(plansInfo) {
  return h("section", { class: "section", id: "pricing" },
    h("div", { class: "container" },
      h("h2", { class: "section-title" }, "Simple pricing"),
      h("p", { class: "section-sub" }, "Try everything free for 3 days. Cancel anytime before it ends and you pay nothing."),
      h("div", { class: "grid-3" }, planCards(plansInfo)),
      h("div", { class: "section-gap" }, checkoutNote()),
    ),
  );
}

// paywall is shown where a feature needs a plan.
function paywall(title, body) {
  const plansInfo = state.plans || { billingEnabled: false };
  return h("div", { class: "card stack paywall" },
    h("div", { class: "stack-sm" }, h("h3", {}, title), h("p", { class: "muted" }, body)),
    h("div", { class: "grid-2" }, planCards(plansInfo, { compact: true })),
    plansInfo.billingEnabled ? checkoutNote() : null,
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
    if (isSignup) body.displayName = data.get("displayName") || "";

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
    isSignup ? h("label", { class: "field" }, "Name", h("input", { type: "text", name: "displayName", autocomplete: "name", maxlength: 120 })) : null,
    h("label", { class: "field" }, "Email", h("input", { type: "email", name: "email", autocomplete: "email", required: true })),
    h("label", { class: "field" }, "Password",
      h("input", { type: "password", name: "password", autocomplete: isSignup ? "new-password" : "current-password", required: true, minlength: isSignup ? 10 : undefined }),
      isSignup ? h("span", { class: "faint small" }, "At least 10 characters.") : link("/forgot-password", { class: "faint small" }, "Forgot password?"),
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
          h("p", { class: "muted" }, isSignup ? "3 days free. You'll add a card to activate, and you won't be charged if you cancel before the trial ends." : "Sign in to continue your search."),
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
    const location = h("input", { type: "text", maxlength: 200, value: draft.location, placeholder: "e.g. Miami, FL" });
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
  renderUsage(usageSlot);

  if (needsActivation() && active !== "billing") {
    navigate("/app/activate", { replace: true });
    return;
  }

  if (active !== "fit" && state.preferences && !state.preferences.onboardingComplete) {
    navigate("/app/welcome", { replace: true });
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

function appShell(active, content, usageSlot) {
  const items = [
    ["home", "/app", "⌂", "My matches"],
    ["jobs", "/app/jobs", "⌕", "Find jobs"],
    ["applications", "/app/applications", "▤", "My applications"],
    ["resumes", "/app/resumes", "⇪", "My résumés"],
    ["profile", "/app/profile", "☺", "My profile"],
    ["billing", "/app/billing", "$", "Plan & billing"],
  ];

  return h("div", { class: "shell" },
    h("aside", { class: "sidebar" },
      brand(),
      h("nav", { "aria-label": "Account" },
        items.map(([key, href, icon, label]) =>
          link(href, { class: key === active ? "nav-link active" : "nav-link", "aria-current": key === active ? "page" : undefined },
            h("span", { class: "nav-icon", "aria-hidden": "true" }, icon), label)),
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
    case "subscription": return billing.cancelAtPeriodEnd && billing.currentPeriodEnd ? `Pro · ends ${formatDate(billing.currentPeriodEnd)}` : "Pro Monthly";
    case "pass": return `Sprint · until ${formatDate(billing.passExpiresAt)}`;
    case "organization": return `Pro via ${billing.organizationName}`;
    case "open": return "Early access";
    default: return "No plan yet";
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
      : link("/app/billing", { class: "btn btn-primary btn-sm" }, trialAvailable() ? "Start free trial" : "Choose a plan"),
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
  const location = h("input", { type: "text", maxlength: 200, value: initial.location || "", placeholder: "e.g. Miami, FL" });

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
  const location = h("input", { type: "text", required: true, maxlength: 200, placeholder: "e.g. Miami, FL", value: (last && last.location) || profile.location || "" });
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
      const matches = await api("/api/v1/job-matches", { method: "POST", body: request });
      const { careerProfile, resumeText: _omit, ...shown } = request;
      state.lastSearch = { request: shown, matches: matches || [], resumeId };
      renderMatches(results, state.lastSearch.matches);
      savePreferences({ minimumSalary: request.minimumSalary, remote: request.remote, hybrid: request.hybrid, onsite: request.onsite }).catch(() => {});
    } catch (error) {
      results.replaceChildren(error.code === "pro_required"
        ? paywall("Your matches are ready to check", "Start your free trial to see every job ranked by how well your experience fits, with what you meet, what's partial, and what's missing.")
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

  const freeNotice = !isPro() && state.billing && state.billing.billingEnabled
    ? h("p", { class: "faint small" }, trialAvailable() ? "Start your 3-day free trial to see your matches. No charge if you cancel before it ends." : "Choose a plan to run searches.")
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

function fitClass(match) {
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

  container.replaceChildren(
    h("p", { class: "muted" }, `${matches.length} role${matches.length === 1 ? "" : "s"}, best fit first`),
    ...matches.map(jobCard),
  );
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

function jobCard(match) {
  const cls = fitClass(match);
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  const meta = [match.location, match.workArrangement, match.employmentType, salaryText(match)].filter(Boolean);

  return h("article", { class: "card job-card" },
    h("div", { class: "job-head" },
      h("div", { class: "stack-sm" },
        h("div", {},
          h("button", { class: "job-title link-button", onclick: () => openJobDetail(match) }, match.title),
          h("div", { class: "muted" }, match.company),
        ),
        meta.length ? h("div", { class: "row" }, meta.map((m) => h("span", { class: "pill" }, m))) : null,
      ),
      h("div", { class: `score ${cls}`, title: match.matchLevel }, `${match.matchPercentage}%`),
    ),
    h("div", { class: "row" },
      h("span", { class: `pill pill-${cls}` }, match.matchLevel || "Fit"),
      h("span", { class: "faint small" },
        `${(match.supportedRequirements || []).length} met · ${(match.partialRequirements || []).length} partial · ${(match.missingRequirements || []).length} missing`),
    ),
    match.explanation ? h("p", { class: "muted" }, match.explanation) : null,
    h("div", { class: "row" },
      h("button", { class: "btn btn-sm", onclick: () => openJobDetail(match) }, "Job details"),
      applyURL ? h("a", { class: "btn btn-primary btn-sm", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, "Apply ↗") : null,
      trackButton(match),
    ),
  );
}

function openJobDetail(match) {
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  const cls = fitClass(match);
  const required = (match.requirements || []).filter((r) => r.importance === "required").map((r) => r.text);
  const preferred = (match.requirements || []).filter((r) => r.importance === "preferred").map((r) => r.text);
  const recommendationSlot = h("div", {});

  openDialog(match.title,
    h("div", { class: "row-between" },
      h("div", {}, h("div", { class: "muted" }, match.company), h("div", { class: "faint small" }, [match.location, salaryText(match)].filter(Boolean).join(" · "))),
      h("div", { class: `score ${cls}` }, `${match.matchPercentage}%`),
    ),
    h("div", { class: "row" },
      applyURL ? h("a", { class: "btn btn-primary btn-sm", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, "Apply ↗") : null,
      trackButton(match),
    ),
    h("div", { class: "card inset stack" },
      h("h3", {}, "Your match"),
      match.explanation ? h("p", { class: "muted" }, match.explanation) : null,
      reqList("supported", "You meet", match.supportedRequirements),
    ),
    gapCloser(gapsFromMatch(match)),
    match.description ? applicationKitSection(async () => {
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
    recommendationCard(match, recommendationSlot),
    required.length || preferred.length ? h("div", { class: "stack-sm" },
      h("h3", {}, "Posting requirements"),
      required.length ? h("div", {}, h("span", { class: "section-label" }, "Required"), h("ul", { class: "plain-list" }, required.map((t) => h("li", {}, t)))) : null,
      preferred.length ? h("div", {}, h("span", { class: "section-label" }, "Preferred"), h("ul", { class: "plain-list" }, preferred.map((t) => h("li", {}, t)))) : null,
    ) : null,
    match.description ? h("div", { class: "stack-sm" }, h("h3", {}, "Job description"), h("div", { class: "description" }, match.description)) : null,
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

async function viewApplications() {
  const params = new URLSearchParams(location.search);
  if (params.get("gmail") === "connected") {
    history.replaceState(null, "", "/app/applications");
    toast("Gmail connected. Sync to update your applications.");
  }

  const [applications, plansInfo] = await Promise.all([api("/api/v1/applications"), loadPlans()]);
  const board = h("div", { class: "board" });

  const draw = () => {
    board.replaceChildren(...COLUMNS.map(([title, statuses]) => {
      const items = applications.filter((a) => statuses.includes(a.status));
      return h("section", { class: "column", "aria-label": title },
        h("div", { class: "column-head" }, h("span", {}, title), h("span", {}, items.length)),
        items.map((application) => applicationCard(application, (updated) => {
          Object.assign(application, updated);
          draw();
        })),
      );
    }));
  };
  draw();

  const reload = async () => {
    const fresh = await api("/api/v1/applications");
    applications.splice(0, applications.length, ...fresh);
    draw();
  };

  const body = applications.length
    ? board
    : h("div", { class: "card empty" },
        h("h3", {}, "Nothing tracked yet"),
        h("p", {}, "Hit “Save” on any job match to add it here."),
        h("div", { class: "row center" }, link("/app/jobs", { class: "btn btn-primary" }, "Find jobs")),
      );

  return h("div", { class: "stack" },
    pageHead("My jobs", "Every status change is saved to the job's timeline."),
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

  const applyURL = safeUrl(job.applyURL || "");

  return h("div", { class: "app-card" },
    h("div", {},
      h("div", { class: "title" }, job.title || "Untitled role"),
      h("div", { class: "muted small" }, [job.company, job.location].filter(Boolean).join(" · ")),
    ),
    select,
    applyURL ? h("a", { class: "small muted", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, "Open posting ↗") : null,
    details,
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
      h("div", {}, h("h3", {}, "Confirm your email"), h("p", { class: "muted small" }, "We sent you a link when you signed up. Confirming lets you reset your password, and unlocks Pro if your school has a Jane Careers license.")),
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
      case "pass": return `Your Sprint gives you Pro until ${formatDate(billing.passExpiresAt)}. It doesn't renew.`;
      case "organization": return `You have Pro through ${billing.organizationName}'s license.`;
      case "open": return "Full access during early access.";
      default: return "Choose a plan to start matching.";
    }
  })();

  sections.push(h("div", { class: "card stack" },
    h("div", { class: "plan-row" },
      h("div", {},
        h("div", { class: "row" }, h("span", { class: "plan-name" }, pro ? planStatusLine(billing) : "No plan yet"), pro && billing.source !== "open" ? h("span", { class: "pill pill-pro" }, billing.source === "trial" ? "TRIAL" : "PRO") : null),
        h("p", { class: "muted" }, detail),
      ),
      billing.canManageBilling ? manage : null,
    ),
  ));

  // Personal plans
  if (!pro || billing.source === "pass") {
    sections.push(h("div", { class: "stack" },
      h("h2", {}, pro ? "Extend your access" : "Choose your plan"),
      plansInfo.billingEnabled
        ? h("div", { class: "grid-2" }, planCards(plansInfo, { compact: true }))
        : h("div", { class: "card muted" }, "Plans are coming soon."),
      plansInfo.billingEnabled ? checkoutNote() : null,
    ));
  }

  // School license
  sections.push(schoolLicenseCard(billing, plansInfo));

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
    h("label", { class: "field" }, "New password", password, h("span", { class: "faint small" }, "At least 10 characters.")),
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
// Activation: a card starts the trial before the app opens
// ---------------------------------------------------------------------------

const FIT_CHECK_KEY = "janecareers.fitCheck";

function needsActivation() {
  return !!(state.billing && state.billing.billingEnabled && !isPro());
}

function afterActivationPath() {
  if (readSavedFitCheck()) return "/app/fit-check";
  if (state.preferences && !state.preferences.onboardingComplete) return "/app/welcome";
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
    for (let attempt = 0; attempt < 8 && !isPro(); attempt++) {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      await loadBilling();
    }
    if (isPro()) {
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
          : "Choose a plan to start matching."),
    ),
    plansInfo.billingEnabled ? h("div", { class: "grid-2" }, planCards(plansInfo, { compact: true })) : null,
    h("ul", { class: "ticks activate-ticks" },
      h("li", {}, "Every requirement checked against your real experience"),
      h("li", {}, "Gap Closer: what to add, what to learn, what to skip"),
      h("li", {}, "Direct links to the employer's own application page"),
      h("li", {}, plansInfo.gmailEnabled ? "Application tracker with Gmail sync" : "Application tracker, from saved to offer"),
    ),
    checkoutNote(),
    h("div", { class: "card inset" }, school),
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
    h("p", { class: "muted" }, message || "Jane found the requirements you're missing. Start your free trial to see them, with a plan for each one."),
    h("div", { class: "row center" },
      link(signedIn ? "/app/activate" : "/signup", { class: "btn btn-primary" }, trialAvailable() ? "Start 3-day free trial" : "Choose a plan")),
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
        ? `Jane found ${data.missingCount + data.partialCount} requirement${data.missingCount + data.partialCount === 1 ? "" : "s"} to work on. Start your free trial to see each one, with what to add, what to learn, and what to skip.`
        : "Start your free trial to see the full requirement-by-requirement breakdown."),
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
// Public Fit Check page
// ---------------------------------------------------------------------------

async function viewFitCheckPublic() {
  if (state.user === undefined) await loadUser();
  if (state.user) {
    navigate("/app/fit-check", { replace: true });
    return;
  }
  await loadPlans();
  mount(h("div", {},
    siteNav(),
    h("main", { class: "section" },
      h("div", { class: "container narrow stack" },
        h("div", { class: "stack-sm center" },
          h("span", { class: "pill eyebrow" }, "Free Fit Check"),
          h("h1", { class: "onboarding-title" }, "Should you apply?"),
          h("p", { class: "muted" }, "Upload your résumé and paste a job. Jane checks every requirement and gives you a straight answer in under a minute. No account needed."),
        ),
        fitCheckTool("public"),
      ),
    ),
    siteFooter(),
  ));
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

function renderKit(kit, title) {
  const copyBtn = (text, label) => h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => copyText(text, label) }, "Copy");
  const parts = [
    h("div", { class: "row-between" },
      h("div", { class: "stack-sm" },
        h("h3", {}, "Your Application Kit"),
        h("span", { class: "truth-badge" }, "✓ Truth-locked: every line comes from your résumé")),
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
      h("ol", { class: "kit-interview" }, kit.interview.map((q) => h("li", {}, h("strong", {}, q.question), h("p", { class: "muted small" }, q.approach))))));
  }
  return h("div", { class: "card inset stack kit" }, parts);
}

// applicationKitSection returns a card with a "Build my Application Kit"
// button. getInput resolves { resumeText, jobDescription, jobTitle, company, gaps }.
function applicationKitSection(getInput, title) {
  const slot = h("div", {});
  const start = h("button", { class: "btn btn-primary", type: "button", onclick: async () => {
    start.disabled = true;
    slot.replaceChildren(h("div", { class: "card inset" }, spinnerRow("Jane is tailoring your résumé to this job, using only what's true…")));
    try {
      const input = await getInput();
      const kit = await api("/api/v1/application-kit", { method: "POST", body: input });
      slot.replaceChildren(renderKit(kit, title));
      intro.remove();
    } catch (error) {
      start.disabled = false;
      slot.replaceChildren(h("div", { class: "form-error" }, error.code === "pro_required" ? "Start your free trial to build Application Kits." : error.message));
    }
  } }, "Build my Application Kit");
  const intro = h("div", { class: "card inset kit-intro stack-sm" },
    h("h3", {}, "Ready to apply? Let Jane tailor your résumé"),
    h("p", { class: "muted small" }, "A résumé summary, bullets and keywords rewritten for this job, plus interview prep for your gaps. Truth-locked: every line traces back to your real résumé, and nothing gets made up."),
    h("div", { class: "row" }, start));
  return h("div", { class: "stack-sm" }, intro, slot);
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
  const matches = await api("/api/v1/job-matches", { method: "POST", body: request });
  const { careerProfile, resumeText: _omit, ...shown } = request;
  state.lastSearch = { request: shown, matches: matches || [], resumeId: primary ? primary.id : "" };
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
          (search.matches || []).length > 5 ? link("/app/jobs", { class: "btn btn-sm" }, `See all ${search.matches.length}`) : null)),
      matches.length
        ? h("div", { class: "stack" }, matches.map(jobCard))
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
      results.replaceChildren(error.code === "pro_required"
        ? paywall("Your matches are ready", "Start your free trial to see every job ranked by how well you fit.")
        : h("div", { class: "form-error" }, error.message));
    }
  };

  // The only question Jane asks: what job, and where.
  const askTarget = (role = "", where = "") => {
    const roleInput = h("input", { type: "text", maxlength: 200, placeholder: "e.g. Registered Nurse", value: role });
    const cityInput = h("input", { type: "text", maxlength: 200, placeholder: "e.g. Miami, FL", value: where });
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

render();
