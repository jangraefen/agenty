import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { type ThemeChoice, themeChoices, useTheme } from "@/theme/theme";

const labels: Record<ThemeChoice, string> = { light: "Light", dark: "Dark", system: "System" };

// ThemeMenu chooses the light or dark theme, or the operating system's. Not
// modal, so the page stays usable while it is open.
export function ThemeMenu() {
  const { theme, choice } = useTheme();
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger className="flex h-8 items-center gap-1 rounded-md border px-2 text-sm whitespace-nowrap">
        Theme: {labels[choice]}
        <span aria-hidden="true">▾</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-32">
        <DropdownMenuRadioGroup
          value={choice}
          onValueChange={(value) => {
            const chosen = themeChoices.find((option) => option === value);
            if (chosen !== undefined) {
              theme.choose(chosen);
            }
          }}
        >
          {themeChoices.map((option) => (
            <DropdownMenuRadioItem key={option} value={option}>
              {labels[option]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
