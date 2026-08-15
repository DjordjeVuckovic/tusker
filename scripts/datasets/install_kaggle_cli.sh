#!/bin/bash
echo "Installing kaggle cli..."
echo "Make sure you have Python and pipx installed."

pipx --version &> /dev/null
if [ $? -ne 0 ]; then
    echo "pipx is not installed. Please install pipx first."
    exit 1
fi

pipx install kaggle

echo "kaggle-cli installation complete."

kaggle --version

echo "You can now use the 'kaggle' command in your terminal! 🌟 ✨"
echo "Don't forget to set up your Kaggle API credentials by placing your 'kaggle.json' file in the '~/.kaggle/' directory. 💉"