# Getting Started with OpenSaaS

Based on the latest documentation, OpenSaaS is a template built on the [Wasp framework](https://wasp-lang.dev/), a declarative, full-stack framework for React and Node.js. Follow these steps to set up your local development environment.

### **Step 1: Prerequisites**

Before you begin, ensure you have the following installed on your system:

-   **Node.js**: Version 18 or higher is recommended.
-   **Docker**: The local database runs in a Docker container, so Docker Desktop must be installed and running.

### **Step 2: Install Wasp**

The first step is to install the Wasp command-line tool (CLI).

1.  Open your terminal and run the following command:
    ```bash
    curl -sSL [https://get.wasp-lang.dev/installer.sh](https://get.wasp-lang.dev/installer.sh) | sh
    ```
2.  After the installation completes, verify it was successful by running:
    ```bash
    wasp --version
    ```

### **Step 3: Create a New Project from the OpenSaaS Template**

Use the `wasp new` command to clone the OpenSaaS starter template into a new project folder.

1.  Navigate to the directory where you want to create your project.
2.  Run the command below to create a new app. Replace `YourBrandVoiceApp` with your desired project name.
    ```bash
    wasp new YourBrandVoiceApp -t saas
    ```
3.  Change into your newly created project directory:
    ```bash
    cd YourBrandVoiceApp
    ```

### **Step 4: Start the Local Database**

Wasp uses Docker to manage a local PostgreSQL database for development.

1.  Ensure Docker Desktop is running.
2.  From your project's root directory, start the database. Keep this terminal window open.
    ```bash
    wasp start db
    ```

### **Step 5: Run the First Database Migration**

Before launching the app, you need to set up the database schema.

1.  Open a **new terminal window or tab** in the same project directory.
2.  Run the `migrate-dev` command:
    ```bash
    wasp db migrate-dev
    ```
    This command reads the schema defined in your Wasp project and applies it to the local database.

### **Step 6: Start the Application**

You are now ready to launch the full-stack application.

1.  In the same terminal where you ran the migration, execute:
    ```bash
    wasp start
    ```
2.  Wasp will build the client and server. Once it's ready, you will see output indicating that the servers are running.
3.  Open your browser and navigate to `http://localhost:3000` to see your running OpenSaaS application.