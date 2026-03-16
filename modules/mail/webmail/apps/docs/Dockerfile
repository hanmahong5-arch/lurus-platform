# --- build ---
FROM oven/bun:1-alpine AS build
WORKDIR /app

# (optional, but helps some native deps on alpine)
RUN apk add --no-cache libc6-compat

# copy app and build
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN bun install && bun run build

# --- runtime ---
FROM node:20-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

# Next.js standalone output
COPY --from=build /app/public ./public
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static

# the standalone bundle includes its own node_modules and package.json
CMD ["node", "server.js"]



#FROM node:20-alpine AS build
#WORKDIR /app
#
#COPY . .
#
## Install deps and build
#RUN npm install && npm run build
#
## --- runtime ---
#FROM node:20-alpine AS runtime
#WORKDIR /app
#ENV NODE_ENV=production
#
#COPY --from=build /app/public ./public
#COPY --from=build /app/.next/standalone ./
#COPY --from=build /app/.next/static ./.next/static
#
#CMD ["node", "server.js"]
