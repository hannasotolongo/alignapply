"use strict";

/*
 * AlignApply web app.
 *
 * No framework and no build step: the Go server embeds and serves these files.
 * All DOM is built with the h() helper, which only ever sets text content, so
 * job descriptions and user input can never inject markup.
 */

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
  const root = document.getElementById("app");
  root.replaceChildren(node);
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
  lastSearch: null,      // { request, matches }
};

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
    state.plans = { billingEnabled: false, plans: [], freeJobMatchesPerWeek: 3 };
  }
  return state.plans;
}

const isPro = () => state.billing && state.billing.plan === "pro";

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

const routes = {
  "/": viewLanding,
  "/pricing": viewLanding,
  "/login": () => viewAuth("login"),
  "/signup": () => viewAuth("signup"),
  "/app": () => navigate("/app/jobs", { replace: true }),
  "/app/jobs": () => appPage("jobs", viewJobs),
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
  return link("/", { class: "brand" }, h("span", { class: "brand-mark" }, "A"), "AlignApply");
}

function siteNav() {
  const signedIn = !!state.user;
  return h("header", { class: "site-nav" },
    h("div", { class: "container" },
      brand(),
      h("nav", { class: "row" },
        link("/pricing", { class: "btn btn-ghost btn-sm" }, "Pricing"),
        signedIn
          ? link("/app/jobs", { class: "btn btn-primary btn-sm" }, "Open app")
          : [
              link("/login", { class: "btn btn-ghost btn-sm" }, "Sign in"),
              link("/signup", { class: "btn btn-primary btn-sm" }, "Get started"),
            ],
      ),
    ),
  );
}

function siteFooter() {
  return h("footer", { class: "site-footer" },
    h("div", { class: "container row-between" },
      h("span", {}, `© ${new Date().getFullYear()} AlignApply`),
      h("span", {}, "Billing is handled securely by Stripe."),
    ),
  );
}

async function viewLanding() {
  if (state.user === undefined) await loadUser();
  const plansInfo = await loadPlans();

  const features = [
    ["◎", "Evidence-based fit", "Every match is checked requirement-by-requirement against your real experience. We never assume a license or degree you don't have."],
    ["⌕", "Search across employers", "We pull roles from job boards and company career sites, filter out noise, and rank what's left by how well you fit."],
    ["▤", "Track every application", "Save roles, log each step from applied to offer, and keep a clean timeline for every application."],
  ];

  mount(h("div", {},
    siteNav(),
    h("main", {},
      h("section", { class: "hero" },
        h("div", { class: "container" },
          h("span", { class: "pill eyebrow" }, "Job matching that shows its work"),
          h("h1", {}, "Apply to jobs you actually fit."),
          h("p", { class: "lead" }, "AlignApply reads each posting's real requirements, checks them against your career profile, and tells you exactly what you meet, what's partial, and what's missing."),
          h("div", { class: "row" },
            link(state.user ? "/app/jobs" : "/signup", { class: "btn btn-primary" }, state.user ? "Open AlignApply" : "Start free"),
            link("/pricing", { class: "btn" }, "See pricing"),
          ),
        ),
      ),
      h("section", { class: "section" },
        h("div", { class: "container grid-3" },
          features.map(([icon, title, body]) =>
            h("div", { class: "card feature" },
              h("div", { class: "feature-icon", "aria-hidden": "true" }, icon),
              h("h3", {}, title),
              h("p", { class: "muted" }, body),
            )),
        ),
      ),
      pricingSection(plansInfo),
    ),
    siteFooter(),
  ));
}

function pricingSection(plansInfo) {
  const freeLimit = plansInfo.freeJobMatchesPerWeek || 3;
  const plans = plansInfo.plans || [];

  const free = h("div", { class: "card price-card" },
    h("div", {}, h("h3", {}, "Free"), h("p", { class: "muted small" }, "Try evidence-based matching.")),
    h("div", { class: "price" }, "$0"),
    h("ul", { class: "ticks" },
      h("li", {}, `${freeLimit} job-match searches per week`),
      h("li", {}, "Career profile"),
      h("li", {}, "Application tracker"),
      h("li", { class: "off" }, "Unlimited searches"),
      h("li", { class: "off" }, "Gmail application sync"),
    ),
    link(state.user ? "/app/jobs" : "/signup", { class: "btn btn-block" }, state.user ? "Open app" : "Start free"),
  );

  const proCards = (plans.length ? plans : [{ key: "monthly", name: "Pro", displayPrice: "—", interval: "month" }])
    .map((plan, index) => h("div", { class: index === plans.length - 1 ? "card price-card featured" : "card price-card" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, plan.name), h("p", { class: "muted small" }, "For an active job search.")),
        index === plans.length - 1 && plans.length > 1 ? h("span", { class: "pill pill-pro" }, "Best value") : null,
      ),
      h("div", { class: "price" }, plan.displayPrice, " ", h("small", {}, `/ ${plan.interval}`)),
      h("ul", { class: "ticks" },
        h("li", {}, "Unlimited job-match searches"),
        h("li", {}, "Career profile"),
        h("li", {}, "Application tracker"),
        h("li", {}, "Resume recommendation"),
        h("li", {}, "Gmail application sync"),
      ),
      h("button", {
        class: "btn btn-primary btn-block",
        disabled: !plansInfo.billingEnabled,
        onclick: () => startCheckout(plan.key),
      }, plansInfo.billingEnabled ? `Get ${plan.name}` : "Coming soon"),
    ));

  return h("section", { class: "section", id: "pricing" },
    h("div", { class: "container" },
      h("h2", { class: "section-title" }, "Simple pricing"),
      h("p", { class: "section-sub" }, "Start free. Upgrade when your search gets serious. Cancel anytime."),
      h("div", { class: proCards.length > 1 ? "grid-3" : "grid-2" }, free, proCards),
    ),
  );
}

async function startCheckout(planKey) {
  if (!state.user) {
    sessionStorage.setItem("alignapply.pendingPlan", planKey);
    navigate("/signup");
    return;
  }
  try {
    const { url } = await api("/api/v1/billing/checkout", { method: "POST", body: { plan: planKey } });
    window.location.assign(url);
  } catch (error) {
    if (error.code === "already_subscribed") {
      toast("You already have Pro.");
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
    navigate("/app/jobs", { replace: true });
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
      state.user = session.user;
      state.billing = null;

      const pendingPlan = sessionStorage.getItem("alignapply.pendingPlan");
      if (pendingPlan) {
        sessionStorage.removeItem("alignapply.pendingPlan");
        await startCheckout(pendingPlan);
        return;
      }
      navigate(isSignup ? "/app/profile" : "/app/jobs");
    } catch (err) {
      error.textContent = err.message;
      error.classList.remove("hidden");
      submit.disabled = false;
    }
  } },
    isSignup ? h("label", { class: "field" }, "Name", h("input", { type: "text", name: "displayName", autocomplete: "name", maxlength: 120 })) : null,
    h("label", { class: "field" }, "Email", h("input", { type: "email", name: "email", autocomplete: "email", required: true })),
    h("label", { class: "field" }, "Password",
      h("input", { type: "password", name: "password", autocomplete: isSignup ? "new-password" : "current-password", required: true, minlength: isSignup ? 10 : undefined }),
      isSignup ? h("span", { class: "faint small" }, "At least 10 characters.") : null,
    ),
    error,
    submit,
  );

  mount(h("div", {}, siteNav(),
    h("div", { class: "auth-wrap" },
      h("div", { class: "card auth-card stack" },
        h("div", { class: "stack-sm" },
          h("h1", {}, isSignup ? "Create your account" : "Welcome back"),
          h("p", { class: "muted" }, isSignup ? "Free to start. No card required." : "Sign in to continue your search."),
        ),
        form,
        h("p", { class: "muted small" },
          isSignup ? "Already have an account? " : "New to AlignApply? ",
          link(isSignup ? "/login" : "/signup", {}, isSignup ? "Sign in" : "Create an account"),
        ),
      ),
    ),
  ));
}

async function logout() {
  try { await api("/api/v1/auth/logout", { method: "POST", allowUnauthorized: true }); } catch { /* ignore */ }
  state.user = null;
  state.billing = null;
  state.profile = null;
  state.lastSearch = null;
  navigate("/");
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

  await loadBilling();
  renderUsage(usageSlot);

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
    ["jobs", "/app/jobs", "Find jobs"],
    ["applications", "/app/applications", "Applications"],
    ["profile", "/app/profile", "Career profile"],
    ["billing", "/app/billing", "Plan & billing"],
  ];

  return h("div", { class: "shell" },
    h("aside", { class: "sidebar" },
      brand(),
      h("nav", { "aria-label": "App" },
        items.map(([key, href, label]) =>
          link(href, { class: key === active ? "nav-link active" : "nav-link", "aria-current": key === active ? "page" : undefined }, label)),
      ),
      h("div", { class: "sidebar-foot" },
        usageSlot,
        h("button", { class: "btn btn-ghost btn-sm", onclick: logout }, "Sign out"),
      ),
    ),
    h("main", { class: "main" }, content),
  );
}

function renderUsage(slot) {
  const billing = state.billing;
  if (!billing) { slot.replaceChildren(); return; }

  const usage = billing.jobMatches || { used: 0, limit: 0, period: "week" };
  const fill = h("span", {});
  fill.style.width = `${Math.min(100, (usage.used / Math.max(1, usage.limit)) * 100)}%`;

  slot.replaceChildren(h("div", { class: "usage-card" },
    h("div", { class: "row-between" },
      h("span", { class: "small muted" }, billing.plan === "pro" ? "AlignApply Pro" : "Free plan"),
      billing.plan === "pro" ? h("span", { class: "pill pill-pro" }, "PRO") : null,
    ),
    billing.plan === "pro"
      ? h("span", { class: "small faint" }, "Unlimited searches")
      : [
          h("div", { class: "meter", role: "progressbar", "aria-valuemin": 0, "aria-valuemax": usage.limit, "aria-valuenow": usage.used }, fill),
          h("span", { class: "small faint" }, `${usage.used} of ${usage.limit} searches this ${usage.period}`),
          link("/app/billing", { class: "btn btn-primary btn-sm" }, "Upgrade"),
        ],
  ));
}

function pageHead(title, subtitle, ...actions) {
  return h("div", { class: "page-head row-between" },
    h("div", {}, h("h1", {}, title), subtitle ? h("p", {}, subtitle) : null),
    actions.length ? h("div", { class: "row" }, actions) : null,
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
  ["license", "Licenses", "Only list licenses you currently hold."],
  ["project", "Projects", ""],
];

async function loadProfile() {
  state.profile = await api("/api/v1/career-profile");
  return state.profile;
}

async function viewProfile() {
  const profile = await loadProfile();

  const headline = h("input", { type: "text", maxlength: 200, value: profile.headline || "", placeholder: "e.g. Backend engineer focused on distributed systems" });
  const summary = h("textarea", { maxlength: 5000, placeholder: "A short summary of your background." });
  summary.value = profile.summary || "";
  const targetRole = h("input", { type: "text", maxlength: 200, value: profile.targetRole || "", placeholder: "e.g. Software Engineer" });
  const location = h("input", { type: "text", maxlength: 200, value: profile.location || "", placeholder: "e.g. Miami, FL" });

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

    (profile.evidence || [])
      .filter((item) => item.category === category)
      .sort((a, b) => a.entryIndex - b.entryIndex)
      .forEach((item) => addRow(item.text));

    return h("div", { class: "card stack-sm" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, label), hint ? h("p", { class: "faint small" }, hint) : null),
        h("button", { class: "btn btn-sm", type: "button", onclick: () => addRow("").focus() }, "+ Add"),
      ),
      list,
    );
  });

  const save = h("button", { class: "btn btn-primary", type: "submit" }, "Save profile");

  const form = h("form", { class: "stack", onsubmit: async (event) => {
    event.preventDefault();
    save.disabled = true;

    const evidence = [];
    for (const [category] of EVIDENCE_SECTIONS) {
      let index = 0;
      const seen = new Set();
      for (const field of lists[category].querySelectorAll("textarea")) {
        const text = field.value.trim();
        if (!text || seen.has(text)) continue;
        seen.add(text);
        evidence.push({ category, entryIndex: index++, text, source: `career_profile.${category}` });
      }
    }

    try {
      state.profile = await api("/api/v1/career-profile", {
        method: "PUT",
        body: {
          headline: headline.value.trim(),
          summary: summary.value.trim(),
          targetRole: targetRole.value.trim(),
          location: location.value.trim(),
          evidence,
        },
      });
      toast("Profile saved");
    } catch (error) {
      toast(error.message, true);
    } finally {
      save.disabled = false;
    }
  } },
    h("div", { class: "card stack" },
      h("label", { class: "field" }, "Headline", headline),
      h("label", { class: "field" }, "Summary", summary),
      h("div", { class: "grid-2" },
        h("label", { class: "field" }, "Target role", targetRole),
        h("label", { class: "field" }, "Location", location),
      ),
    ),
    sections,
    h("div", { class: "row" }, save),
  );

  return h("div", {},
    pageHead("Career profile", "Matches are checked against what you put here. Only list what's true. That's what makes the scores trustworthy."),
    form,
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
// Job matches
// ---------------------------------------------------------------------------

async function viewJobs() {
  const profile = state.profile || await loadProfile();
  const hasEvidence = (profile.evidence || []).length > 0 || (profile.summary || "").trim() !== "";

  if (!hasEvidence) {
    return h("div", {},
      pageHead("Find jobs"),
      h("div", { class: "card empty" },
        h("h3", {}, "Start with your career profile"),
        h("p", {}, "Add your experience and skills so we can check each job's requirements against them."),
        h("div", { class: "row", }, link("/app/profile", { class: "btn btn-primary" }, "Build my profile")),
      ),
    );
  }

  const last = state.lastSearch && state.lastSearch.request;
  const targetRole = h("input", { type: "text", required: true, maxlength: 200, value: (last && last.targetRole) || profile.targetRole || "" });
  const location = h("input", { type: "text", required: true, maxlength: 200, value: (last && last.location) || profile.location || "" });
  const minimumSalary = h("input", { type: "text", inputmode: "numeric", placeholder: "Optional", value: (last && last.minimumSalary) || "" });
  const remote = h("input", { type: "checkbox" });
  const hybrid = h("input", { type: "checkbox" });
  const onsite = h("input", { type: "checkbox" });
  if (last) { remote.checked = last.remote; hybrid.checked = last.hybrid; onsite.checked = last.onsite; }

  const results = h("div", { class: "stack" });
  const submit = h("button", { class: "btn btn-primary", type: "submit" }, "Find matches");

  const form = h("form", { class: "card stack", onsubmit: async (event) => {
    event.preventDefault();
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
      toast("Target role and location are required", true);
      return;
    }

    submit.disabled = true;
    results.replaceChildren(h("div", { class: "card" }, spinnerRow("Searching employers and checking every requirement. This can take up to 20 seconds…")));

    try {
      const matches = await api("/api/v1/job-matches", { method: "POST", body: request });
      state.lastSearch = { request, matches: matches || [] };
      renderMatches(results, state.lastSearch.matches);
    } catch (error) {
      results.replaceChildren(searchError(error));
    } finally {
      submit.disabled = false;
      await loadBilling();
      const slot = document.querySelector(".sidebar-foot > div");
      if (slot) renderUsage(slot);
    }
  } },
    h("div", { class: "grid-3" },
      h("label", { class: "field" }, "Target role", targetRole),
      h("label", { class: "field" }, "Location", location),
      h("label", { class: "field" }, "Minimum salary", minimumSalary),
    ),
    h("div", { class: "row-between" },
      h("div", { class: "row" },
        h("label", { class: "check" }, remote, "Remote"),
        h("label", { class: "check" }, hybrid, "Hybrid"),
        h("label", { class: "check" }, onsite, "On-site"),
      ),
      submit,
    ),
  );

  if (state.lastSearch) renderMatches(results, state.lastSearch.matches);

  const freeNotice = !isPro() && state.billing
    ? h("p", { class: "faint small" }, `Free plan: ${state.billing.jobMatches.used} of ${state.billing.jobMatches.limit} searches used this week.`)
    : null;

  return h("div", { class: "stack" },
    pageHead("Find jobs", "Ranked by how well your real experience covers each role's requirements."),
    form,
    freeNotice,
    results,
  );
}

function searchError(error) {
  if (error.code === "free_limit_reached") {
    return h("div", { class: "card upsell" },
      h("div", {}, h("h3", {}, "You've used this week's free searches"), h("p", { class: "muted" }, "Upgrade to Pro for unlimited job-match searches.")),
      link("/app/billing", { class: "btn btn-primary" }, "Upgrade to Pro"),
    );
  }
  return h("div", { class: "form-error" }, error.message);
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
      h("h3", {}, "No matches found"),
      h("p", {}, "Try a broader role title, a nearby city, or enable remote roles."),
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

function jobCard(match) {
  const cls = fitClass(match);
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  const meta = [match.location, match.workArrangement, match.employmentType, salaryText(match)].filter(Boolean);

  const trackButton = h("button", { class: "btn btn-sm", onclick: async () => {
    trackButton.disabled = true;
    try {
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
      trackButton.textContent = "Tracked ✓";
      toast("Added to your applications");
    } catch (error) {
      trackButton.disabled = false;
      toast(error.message, true);
    }
  } }, "Track");

  return h("article", { class: "card job-card" },
    h("div", { class: "job-head" },
      h("div", { class: "stack-sm" },
        h("div", {},
          h("div", { class: "job-title" }, match.title),
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
    h("details", { class: "more" },
      h("summary", {}, "Requirement breakdown"),
      h("div", { class: "stack" },
        reqList("supported", "You meet", match.supportedRequirements),
        reqList("partial", "Partially", match.partialRequirements),
        reqList("missing", "Missing", match.missingRequirements),
      ),
    ),
    h("div", { class: "row" },
      applyURL ? h("a", { class: "btn btn-primary btn-sm", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, "Apply ↗") : null,
      trackButton,
    ),
  );
}

// ---------------------------------------------------------------------------
// Applications
// ---------------------------------------------------------------------------

const STATUS_LABELS = {
  saved: "Saved",
  applied: "Applied",
  received: "Received",
  recruiter_contact: "Recruiter contact",
  interviewing: "Interviewing",
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
  ["Interviewing", ["interviewing"]],
  ["Outcome", ["offer", "hired", "rejected", "withdrawn"]],
];

async function viewApplications() {
  const applications = await api("/api/v1/applications");

  if (!applications.length) {
    return h("div", {},
      pageHead("Applications"),
      h("div", { class: "card empty" },
        h("h3", {}, "Nothing tracked yet"),
        h("p", {}, "Hit “Track” on any job match to add it here."),
        h("div", { class: "row" }, link("/app/jobs", { class: "btn btn-primary" }, "Find jobs")),
      ),
    );
  }

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

  return h("div", {},
    pageHead("Applications", "Every status change is saved to the application's timeline."),
    board,
  );
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
        ? events.map((e) => h("li", {}, `${new Date(e.occurredAt).toLocaleDateString()} · ${STATUS_LABELS[e.newStatus] || e.newStatus}${e.summary ? ` · ${e.summary}` : ""}`))
        : [h("li", {}, `Saved ${new Date(application.createdAt).toLocaleDateString()}`)]));
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
  if (checkout) {
    history.replaceState(null, "", "/app/billing");
    if (checkout === "success") {
      toast("Payment received. Welcome to Pro!");
      // The webhook may land a moment after the redirect.
      for (let attempt = 0; attempt < 6 && !isPro(); attempt++) {
        await new Promise((resolve) => setTimeout(resolve, 1500));
        await loadBilling();
      }
      const slot = document.querySelector(".sidebar-foot > div");
      if (slot) renderUsage(slot);
    }
  }

  const billing = state.billing || await loadBilling();
  const plansInfo = await loadPlans();
  const pro = isPro();

  const statusLine = (() => {
    if (!pro) return "Free plan";
    if (!billing.currentPeriodEnd) return "Active";
    const date = new Date(billing.currentPeriodEnd).toLocaleDateString(undefined, { dateStyle: "medium" });
    if (billing.cancelAtPeriodEnd) return `Cancels on ${date}`;
    if (billing.status === "past_due") return "Payment past due. Update your card to keep Pro.";
    return `Renews on ${date}`;
  })();

  const openPortal = async (button) => {
    button.disabled = true;
    try {
      const { url } = await api("/api/v1/billing/portal", { method: "POST" });
      window.location.assign(url);
    } catch (error) {
      button.disabled = false;
      toast(error.message, true);
    }
  };

  const manage = h("button", { class: "btn", onclick: () => openPortal(manage) }, "Manage billing");

  const current = h("div", { class: "card stack" },
    h("div", { class: "plan-row" },
      h("div", {},
        h("div", { class: "row" }, h("span", { class: "plan-name" }, pro ? "AlignApply Pro" : "Free"), pro ? h("span", { class: "pill pill-pro" }, "PRO") : null),
        h("p", { class: "muted" }, statusLine),
      ),
      billing.canManageBilling ? manage : null,
    ),
    h("p", { class: "faint small" },
      pro
        ? `Unlimited job-match searches (fair-use limit ${billing.jobMatches.limit} per hour).`
        : `${billing.jobMatches.used} of ${billing.jobMatches.limit} free searches used this week.`),
  );

  const upgrade = !pro
    ? h("div", { class: "stack" },
        h("h2", {}, "Upgrade to Pro"),
        !plansInfo.billingEnabled
          ? h("div", { class: "card muted" }, "Billing isn't configured on this server yet.")
          : h("div", { class: "grid-2" }, plansInfo.plans.map((plan) => {
              const button = h("button", { class: "btn btn-primary btn-block", onclick: async () => {
                button.disabled = true;
                await startCheckout(plan.key);
                button.disabled = false;
              } }, `Choose ${plan.name}`);
              return h("div", { class: "card price-card" },
                h("h3", {}, plan.name),
                h("div", { class: "price" }, plan.displayPrice, " ", h("small", {}, `/ ${plan.interval}`)),
                h("ul", { class: "ticks" },
                  h("li", {}, "Unlimited job-match searches"),
                  h("li", {}, "Resume recommendation"),
                  h("li", {}, "Gmail application sync"),
                ),
                button,
              );
            })),
      )
    : null;

  const danger = h("div", { class: "card stack-sm" },
    h("h3", {}, "Delete account"),
    h("p", { class: "muted small" }, "Permanently deletes your profile, tracked applications, and cancels any active subscription immediately."),
    h("div", { class: "row" }, h("button", { class: "btn btn-danger btn-sm", onclick: deleteAccount }, "Delete my account")),
  );

  return h("div", { class: "stack" },
    pageHead("Plan & billing", state.user.email ? `Signed in as ${state.user.email}` : null),
    current,
    upgrade,
    danger,
  );
}

async function deleteAccount() {
  const confirmation = window.prompt("This permanently deletes your account and cancels your subscription. Type DELETE to confirm.");
  if (confirmation !== "DELETE") return;
  try {
    await api("/api/v1/account", { method: "DELETE" });
    state.user = null;
    state.billing = null;
    toast("Your account has been deleted");
    navigate("/");
  } catch (error) {
    toast(error.message, true);
  }
}

// ---------------------------------------------------------------------------

render();
