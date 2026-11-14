import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./styles.css";
import { installBenchHooks } from "./bench";

installBenchHooks();

ReactDOM.createRoot(document.getElementById("root")!).render(<App />);
