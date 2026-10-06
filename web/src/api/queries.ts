import { queryOptions } from "@tanstack/react-query";
import { type Api, unwrap } from "./client";

export function meQuery(api: Api) {
  return queryOptions({
    queryKey: ["me"],
    queryFn: () => unwrap(api.GET("/v1/me")),
  });
}
