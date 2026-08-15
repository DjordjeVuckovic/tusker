#!/bin/bash

ls ~/.kaggle/kaggle.json &> /dev/null
if [ $? -ne 0 ]; then
    echo "Kaggle API credentials not found."
    echo "Please follow these steps to set up your Kaggle API credentials:"
    echo "1) Go to https://www.kaggle.com/ and log in to your account."
    echo "2) Click on your profile picture in the top right corner and select 'Profile/Settings'."
    echo "3) Scroll down to the 'API' section and click on 'Create New Token'."
    echo "4) This will download a file named 'kaggle.json'."
    echo "5) Move the 'kaggle.json' file to the '~/.kaggle/' directory (create the directory if it doesn't exist): mkdir -p ~/.kaggle && mv ~/Downloads/kaggle.json ~/.kaggle/ "
    echo "6) Set the file permissions to read-only using the command: chmod 600 ~/.kaggle/kaggle.json"
    exit 1
fi

echo "Choose a dataset to download from Kaggle:"
echo "1) Global News Dataset"
echo "2) News Category Dataset"
echo "3) Hacker News Posts"
read -r -p "Enter the number of your choice: " choice

if [ "$choice" -eq 1 ]; then
    dataset="rmisra/news-category-dataset"
elif [ "$choice" -eq 2 ]; then
    dataset="everydaycodings/global-news-dataset"
elif [ "$choice" -eq 3 ]; then
    dataset="hacker-news/hacker-news-posts"
else
    echo "Invalid choice. Exiting."
    exit 1
fi

# Download and unzip the dataset
kaggle datasets download "$dataset" --unzip -p ../dataset/kaggle

echo "Dataset downloaded and extracted to dataset/kaggle 🎇"