FROM mcr.microsoft.com/playwright:v1.49.1-jammy
WORKDIR /work
COPY tests/smoke/package.json ./
RUN npm install
COPY tests/smoke/ /work/
CMD ["npx", "playwright", "test"]
