FROM mcr.microsoft.com/playwright:v1.49.1-jammy
WORKDIR /work
COPY tests/smoke/package.json ./
RUN npm install
COPY tests/smoke/ /work/
ENV SERVEMEDIA_BASE_URL=http://servemedia:7676
CMD ["npx", "playwright", "test"]
