const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: '.',
  timeout: 120000,
  workers: 1,
  use: {
    baseURL: process.env.SERVEMEDIA_BASE_URL || 'http://servemedia:7676',
    headless: true,
    launchOptions: {
      args: ['--autoplay-policy=no-user-gesture-required'],
    },
  },
  reporter: 'list',
});
