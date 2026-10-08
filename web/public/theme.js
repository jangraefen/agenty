// Applies the user's stored theme before the first paint, so the page never
// shows the wrong one. index.html loads it as a classic script in <head>,
// which runs at once, where the app's module script runs only after parsing;
// the Content-Security-Policy allows it as one of the app's own scripts, as it
// would not an inline one. It reads the choice as src/theme/theme.ts keeps it,
// and src/theme/theme.test.ts checks the two agree.
(() => {
  let choice = null;
  try {
    choice = localStorage.getItem("agenty.theme");
  } catch {
    // Storage refused: nothing was stored, so System applies.
  }
  const dark =
    choice === "dark" ||
    (choice !== "light" &&
      typeof matchMedia === "function" &&
      matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
})();
