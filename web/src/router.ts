import { createRouter, createRoute, createRootRoute } from "@tanstack/react-router";
import RootLayout from "./routes/__root";
import IndexPage from "./routes/index";
import UseCasePage from "./routes/use-case";

const rootRoute = createRootRoute({ component: RootLayout });

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: IndexPage,
});

const useCaseRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/use-case",
  component: UseCasePage,
});

const routeTree = rootRoute.addChildren([indexRoute, useCaseRoute]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
